# AI Employee Platform 安装与部署手册

本手册详细指导如何在宿主机/服务器上部署 **中心服务器（Control Plane Stack）** 以及在开发机（macOS / Linux）上安装配置 **计算工作站（Workstation Daemon）**，实现端到端基于 mTLS 双向认证的安全协同。

---

## 目录
- [一、系统架构与组件拓扑](#一系统架构与组件拓扑)
- [二、环境前置准备](#二环境前置准备)
- [三、中心服务器安装与部署](#三中心服务器安装与部署)
  - [3.1 Docker 一键容器化部署（生产推荐）](#31-docker-一键容器化部署生产推荐)
  - [3.2 首次部署向导（Setup Wizard）配置](#32-首次部署向导setup-wizard配置)
- [四、计算工作站（Workstation）安装与接入](#四计算工作站workstation安装与接入)
  - [4.1 构建工作站 CLI/Daemon 程序](#41-构建工作站-clidaemon-程序)
  - [4.2 获取工作站接入令牌（Enrollment Token）](#42-获取工作站接入令牌enrollment-token)
  - [4.3 执行工作站身份注册与 mTLS 证书签发](#43-执行工作站身份注册与-mtls-证书签发)
  - [4.4 启动工作站守护进程并验证状态](#44-启动工作站守护进程并验证状态)
  - [4.5 配置工作站开机自启（macOS / Linux / Windows）](#45-配置工作站开机自启macos--linux--windows)
- [五、端到端业务闭环验证](#五端到端业务闭环验证)
- [六、常见运维与故障排查 FAQ](#六常见运维与故障排查-faq)

---

## 一、系统架构与组件拓扑

平台采用 **中心化管控面（Control Plane） + 分布式计算工作站（Workstation）** 的混合分布式架构：

```
                           +--------------------------------------------------+
                           |               中心服务器 (Control Plane)           |
                           |                                                  |
                           |  +---------------+  +-------------------------+  |
                           |  |  aie-postgres |  |    aie-control-plane    |  |
                           |  |  (:5432)      |  |    - REST API (:8080)   |  |
                           |  |  数据持久化    |  |    - mTLS gRPC (:9090)  |  |
                           |  +-------^-------+  |    - 内置安全 CA 中心   |  |
                           |          |          +------------^------------+  |
                           |          +-----------------------+               |
                           |                                  |               |
                           |                     +------------+------------+  |
                           |                     |        aie-admin        |  |
                           |                     |    - Nginx Web (:8088)  |  |
                           |                     |    - React 19 SPA       |  |
                           |                     +-------------------------+  |
                           +----------------------------------^---------------+
                                                              |
                                                    mTLS gRPC (:9090)
                                                              |
                               +------------------------------v-------------------+
                               |          开发工作站 (Workstation Daemon)          |
                               |                                                  |
                               |  - aew daemon 后台常驻服务进程                     |
                               |  - 本机硬件监控 (CPU/内存/磁盘)                    |
                               |  - 独立客户端证书 (~/.aie/identity)               |
                               |  - Provider 执行引擎 (Cursor / Codex / ACP)      |
                               |  - 本地工作区隔离与文件交互                       |
                               +--------------------------------------------------+
```

### 核心网络端口清单
| 端口 | 协议 | 组件 | 用途说明 | 安全建议 |
| :--- | :--- | :--- | :--- | :--- |
| **8088** | TCP / HTTP | `aie-admin` | 管理后台 Web 界面 | 对管理员内网开放 |
| **8080** | TCP / HTTP | `aie-control-plane` | 中心 REST API & SSE 事件流 | 对 Web 及管理工具开放 |
| **9090** | TCP / gRPC | `aie-control-plane` | 工作站双向 mTLS 长连服务 | 对所有工作站网络开放 |
| **5432** | TCP / PGSQL | `aie-postgres` | PostgreSQL 主数据库 | 仅限容器内网访问，勿公网暴露 |

---

## 二、环境前置准备与目录规划

#### 2.1 中心服务器宿主机
- **操作系统**：Linux（Ubuntu 22.04+ / Debian 12+ / CentOS 8+）或 macOS（13.0+）
- **软件依赖**：
  - Docker 24.0+
  - Docker Compose v2.20+
  - （如需源码编译）Go 1.22+，Node.js 20+

#### 2.2 计算工作站主机
- **操作系统**：
  - **Windows**（Windows 10 / 11 64位，Windows Server 2019+）
  - **macOS**（Apple Silicon M系列 / Intel 13.0+）
  - **Linux**（Ubuntu 20.04+、Debian 11+、CentOS/RHEL 8+ 等 x86_64 / aarch64）
- **软件依赖**：
  - Go 1.22+（用于本地源码编译；或直接使用预编译生成的二进制包）
  - Git（代码版本管理与工作区交互，Windows 请安装 Git for Windows）
  - 命令行终端：PowerShell 5.1+ / 7+（Windows 推荐），或 Bash / Zsh（macOS / Linux）
  - （可选）日常 IDE：Cursor / VS Code
  - **（执行 Agent 必需）** Cursor CLI：`agent` 命令可用（`~/.local/bin/agent`），用于 `agent acp` 无头 ACP，而非打开 Cursor.app

#### 2.3 部署目录结构与权限规划清单

在开始部署前，请了解中心服务器与工作站各需要创建哪些目录：

##### A. 中心服务器（Control Plane Stack）
| 部署方式 | 目录路径 | 属主与建议权限 | 用途说明 |
| :--- | :--- | :--- | :--- |
| **Docker Compose（推荐）** | `/opt/ai-employee-platform` (或任意工作目录) | `deploy:deploy` 或当前用户 `0755` | 存放项目源码、`docker-compose.yml` 及构建文件 |
| **Postgres 数据持久卷** | Docker 命名卷 `aie_pg_data` (由 Docker 自动托管) | 内部 `postgres:postgres (0700)` | 数据库全量持久化数据目录 |
| **任务制品归档（可选）** | `/var/lib/aie/artifacts` (映射环境变量 `AIE_ARTIFACT_DIR`) | `aie:aie (0750)` | 存放数字员工任务产物、代码补丁和日志产物 |
| **机密凭证落盘（可选）** | `/var/lib/aie/secrets` (映射环境变量 `AIE_SECRET_DIR`) | `aie:aie (0700)` | 启用 FileVault 时存储落盘 AES-GCM 加密机密文件 |
| **主机日志** | `/var/log/aie` | `aie:aie (0755)` | 物理机或容器日志归档（若使用 Docker 日志驱动则可由 Docker 统一管理） |

##### B. 计算工作站（Workstation / aew）
工作站端分为 **用户级目录模式（推荐，免 sudo，不污染系统）** 与 **系统级默认模式**：

| 操作系统 | 推荐目录模式 (`AIE_DATA_DIR`) | 核心子目录结构 | 用途说明 |
| :--- | :--- | :--- | :--- |
| **macOS** | **`$HOME/.aie/`**<br>*(执行 `aew service install` 时自动配置)* | • `~/.aie/identity/`<br>• `~/.aie/config/`<br>• `~/.aie/employees/`<br>• `~/.aie/logs/` | • `identity/`: 存放 `client.key` (本地私钥 0600)、`client.crt`、`ca.crt`、`workstation_id`<br>• `employees/`: 数字员工隔离工作区（代码工程与任务沙箱）<br>• `logs/`: `daemon.log` 运行日志 |
| **Linux** | **`$HOME/.aie/`**<br>*(免 root 推荐)*<br>或系统级 `/var/lib/aie/` | • `~/.aie/identity/`<br>• `~/.aie/employees/`<br>• `~/.aie/logs/` | • 普通用户运行 `systemctl --user` 托管，免 root 权限<br>• 若为系统级服务：配置 `/etc/aie`，证书 `/var/lib/aie/identity` |
| **Windows** | **`%USERPROFILE%\.aie`**<br>或系统级 `C:\ProgramData\AIEmployee\` | • `identity\`<br>• `config\`<br>• `logs\`<br>• 默认工作区 `D:\AIEmployees` (或自定义) | • `identity\`: 存放本机的 mTLS 客户端数字证书与私钥<br>• 工作区：数字员工执行 Git Clone 与编程任务的本地工作盘 |

> [!TIP]
> **目录无需手动预先 `mkdir`**：`aew register` 和 `aew service install` 会根据 `AIE_DATA_DIR`（默认 `~/.aie`）在注册和安装时**全自动按需创建上述全部目录与权限**。用户只需保证该磁盘目录具备当前用户的写权限即可。

---

## 三、中心服务器安装与部署

### 3.1 Docker 一键容器化部署（生产推荐）

1. **获取代码仓库**：
   ```bash
   git clone https://github.com/ai-employee-platform/ai-employee-platform.git
   cd ai-employee-platform
   ```

2. **检查环境配置（可选）**：
   配置文件位于 `deploy/docker-compose.yml`。生产环境下默认已配置为：
   - `AIE_ENV: production`（正式生产模式，无假数据注入）
   - `AIE_SEED_DEMO: "0"`（禁用任何演示数据）
   - `AIE_HTTP_ADDR: ":8080"`
   - `AIE_GRPC_ADDR: ":9090"`

3. **构建并启动全套容器栈**：
   ```bash
   docker compose -f deploy/docker-compose.yml build
   docker compose -f deploy/docker-compose.yml up -d
   ```

4. **确认容器运行状态**：
   ```bash
   docker compose -f deploy/docker-compose.yml ps
   ```
   输出应展示全部容器处于运行中：
   ```
   NAME                IMAGE                  STATUS                 PORTS
   aie-admin           deploy-admin-web       Up                     0.0.0.0:8088->80/tcp
   aie-control-plane   deploy-control-plane   Up                     0.0.0.0:8080->8080/tcp, 0.0.0.0:9090->9090/tcp
   aie-postgres        postgres:16-alpine     Up (healthy)           0.0.0.0:5432->5432/tcp
   ```

5. **执行/确认数据库架构迁移（自动初始化）**：
   数据库首次启动时，可执行内嵌迁移程序确保 34 张标准架构表就绪：
   ```bash
   docker exec -i aie-postgres psql -U aie -d aie -c "\dt"
   ```

---

### 3.2 首次部署向导（Setup Wizard）配置

中心服务器启动后，为了保证安全性，系统**不包含任何硬编码管理员账号**，必须通过首次部署向导进行初始化：

1. 打开浏览器访问控制台：
   ```
   http://<中心服务器IP>:8088/
   ```
2. 系统会自动检测到未初始化状态，并无缝重定向至首次部署向导：
   ```
   http://<中心服务器IP>:8088/setup
   ```
3. 在页面中填写初始超级管理员配置：
   - **平台/实例名称**：例如 `AI Employee 数字员工管控中心`
   - **超级管理员账号**：输入自定义账号（例如 `admin` 或企业管理主账号）
   - **管理员昵称**：例如 `系统管理员`
   - **管理员登录密码**：设置安全密码（至少 8 位，包含字符与数字）
   - **确认登录密码**：再次确认密码
4. 点击 **“完成初始化并进入控制台”**。
   - 系统将创建超级管理员，自动保存登录凭据并进入主控制台。
   - **安全锁定保护**：初始化接口（`/api/setup/init`）完成初始化后将**永久关闭并锁定**，后续所有重入请求均返回 `409 Conflict`，杜绝任何外部攻击者重置或提权可能。

---

## 四、计算工作站（Workstation）安装与接入

计算工作站作为实际承载研发任务和本地 IDE / Agent 操作的计算节点，通过双向 mTLS 证书安全接入中心控制面。

### 4.1 构建工作站 CLI/Daemon 程序

根据工作站主机的操作系统，进入 `workstation` 目录执行构建：

**Windows 环境（PowerShell）**：
```powershell
cd workstation
go build -o bin\aew.exe .\cmd\aew
```

**macOS / Linux 环境（Bash / Zsh）**：
```bash
cd workstation
go build -o bin/aew ./cmd/aew
```

**跨平台交叉编译（在任意机器上为 Windows 构建 `aew.exe`）**：
```bash
cd workstation
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/aew.exe ./cmd/aew
```

**配置全局命令软链接（全平台支持，推荐）**：
编译完成后，执行以下命令即可自动在系统全局 PATH 目录建立软链接：
```bash
# macOS / Linux / Windows
./bin/aew link
```
执行后程序会自动根据当前系统探测并建立全局软链接（macOS/Linux 为 `/opt/homebrew/bin/aew`、`/usr/local/bin/aew` 或 `~/.local/bin/aew`；Windows 为系统 PATH 脚本），之后在任何目录下均可直接输入 `aew` 运行！
*(注：后续执行 `aew service install` 时也会自动建立此全局软链接)*

---

### 4.2 获取工作站接入令牌（Enrollment Token）

接入工作站需要向中心服务器申请一次性安全注册令牌（Token）。

**方式一：在 Admin Web 后台图形化一键生成（强烈推荐）**：
1. 使用超级管理员登录管理后台 `http://<中心服务器IP>:8088`。
2. 点击左侧导航栏第 3 项 **「工作站节点」**（页面 URL：`http://<中心服务器IP>:8088/workstations`）。
3. 点击页面右上角的绿色按钮 **「➕ 接入新工作站」**（若当前没有任何节点，也可直接点击页面正中央的“立即接入第一台工作站”按钮）。
4. 在展开的接入向导中：
   - 输入节点备注名称（如 `我的办公机-Mac` 或 `开发计算节点-Windows`）。
   - 设置令牌有效期（默认 24 小时，单次使用有效）。
   - 确认中心服务器 IP 与端口（默认自动识别当前访问的服务器地址）。
5. 点击 **「生成一次性接入令牌」**。
   - 页面将直接生成单次有效的唯一 Token。
   - 页面下方将自动生成适用于 **macOS / Linux** 与 **Windows** 的完整一键注册命令，支持一键复制代码直接到终端中粘贴运行！

**方式二：通过 API 命令行直接生成**：
```bash
# 1. 登录获取 Admin 会话 Token
ADMIN_TOKEN=$(curl -s -X POST http://<中心服务器IP>:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"<你的管理员账号>","password":"<你的管理员密码>"}' | jq -r .token)

# 2. 生成工作站 Enrollment Token
ENROLL_TOKEN=$(curl -s -X POST http://<中心服务器IP>:8080/api/enrollment/tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"label":"macOS-Dev-Node","ttl_hours":24}' | jq -r .token)

echo "注册令牌: $ENROLL_TOKEN"
```

---

### 4.3 执行工作站身份注册与 mTLS 证书签发

在工作站主机上执行 `register` 命令，向中心服务器申请独有的客户端证书与硬件工作站标识（Workstation ID）：

**macOS / Linux 环境**：
```bash
# 环境变量指定数据保存目录，默认建议使用 ~/.aie
export AIE_DATA_DIR="$HOME/.aie"

# 执行配对注册
./bin/aew register \
  --server http://<中心服务器IP>:8080 \
  --token "<你的ENROLL_TOKEN>" \
  --grpc-target "<中心服务器IP>:9090"
```

**Windows 环境（PowerShell）**：
```powershell
# 环境变量指定数据保存目录，默认建议使用 $env:USERPROFILE\.aie
$env:AIE_DATA_DIR = "$env:USERPROFILE\.aie"

# 执行配对注册
.\bin\aew.exe register `
  --server http://<中心服务器IP>:8080 `
  --token "<你的ENROLL_TOKEN>" `
  --grpc-target "<中心服务器IP>:9090"
```

**Windows 环境（CMD）**：
```cmd
set AIE_DATA_DIR=%USERPROFILE%\.aie
.\bin\aew.exe register --server http://<中心服务器IP>:8080 --token "<你的ENROLL_TOKEN>" --grpc-target "<中心服务器IP>:9090"
```

**注册成功输出示例**：
```
注册成功！
Workstation ID : WS-6a11298e-b404-4b6a-8a33-0de769999877
证书已保存在   : /Users/username/.aie/identity (Windows 为 %USERPROFILE%\.aie\identity)
gRPC 目标地址  : <中心服务器IP>:9090
```

注册完成后，工作站的身份文件已安全落盘在 `~/.aie/identity/`（Windows 为 `%USERPROFILE%\.aie\identity\`）：
- `ca.crt`：中心 CA 根证书
- `tls.crt`：中心 CA 签发的工作站客户端证书（CN 包含工作站 ID）
- `tls.key`：工作站本地生成的私钥（不离开本机）

---

### 4.4 启动工作站守护进程并验证状态

1. **在前台启动工作站守护进程（便于查看实时日志）**：
   - **macOS / Linux**：
     ```bash
     AIE_DATA_DIR="$HOME/.aie" ./bin/aew daemon
     ```
   - **Windows (PowerShell)**：
     ```powershell
     $env:AIE_DATA_DIR = "$env:USERPROFILE\.aie"
     .\bin\aew.exe daemon
     ```
   - **Windows (CMD)**：
     ```cmd
     set AIE_DATA_DIR=%USERPROFILE%\.aie
     .\bin\aew.exe daemon
     ```

   **正常运行日志示例**：
   ```
   aew daemon 启动成功 (version=0.0.1-dev workstation=WS-6a11298e...)
   硬件健康探针已就绪: CPU, 内存, 磁盘空间
   已连接中心控制面 gRPC (mTLS): <中心服务器IP>:9090 [CONNECTED]
   正在向控制面同步心跳 (周期 5s)...
   ```

2. **在另一个终端查询工作站实时状态**：
   - **macOS / Linux**：`./bin/aew status`
   - **Windows**：`.\bin\aew.exe status`

   输出应展示当前连接正常且指标实时采集：
   ```
   Mode           : daemon
   reconnect      : CONNECTED
   ready          : true
   cpu_percent    : 1.5
   mem_percent    : 0.04
   disk_percent   : 0.12
   providers      : [cursor codex]
   workspaces     : 0
   sessions       : 0
   jobs           : 0
   version        : 0.0.1-dev
   ```

---

### 4.5 配置工作站开机自启（macOS / Linux / Windows）

`aew` 针对主流操作系统均内置了原生系统服务管理器（`aew service` 命令），支持**跨平台一键安装、启动、重启与状态查询**。同时亦支持手动编写服务配置文件。

#### 选项 A：macOS 环境（内置 launchd 托管）

**方式 1：使用内置服务命令（推荐，免 root，写入当前用户 LaunchAgents）**：
```bash
# 1. 一键安装为用户级后台守护服务（自动生成 ~/Library/LaunchAgents/com.aie.aew.plist 并加载）
./bin/aew service install

# 2. 启动服务
./bin/aew service start

# 3. 检查服务运行状态
./bin/aew service status

# 4. 如需重启或停止服务：
./bin/aew service restart
./bin/aew service stop

# 5. 卸载服务：
./bin/aew service uninstall
```

**方式 2：手动创建 plist 文件（可选）**：
创建用户级服务配置文件 `~/Library/LaunchAgents/com.aie.workstation.plist`：
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.aie.workstation</string>
    <key>ProgramArguments</key>
    <array>
        <!-- 替换为你的 aew 二进制完整绝对路径 -->
        <string>/usr/local/bin/aew</string>
        <string>daemon</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>AIE_DATA_DIR</key>
        <string>/Users/your_username/.aie</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Users/your_username/.aie/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/your_username/.aie/daemon_err.log</string>
</dict>
</plist>
```
加载并启动守护进程：`launchctl load ~/Library/LaunchAgents/com.aie.workstation.plist`。

#### 选项 B：Linux 环境（内置 systemd 托管）

**方式 1：使用内置服务命令（推荐）**：
`aew` 能够自动探测当前权限环境：非 root 用户写入 `~/.config/systemd/user/` 并调用 `systemctl --user`；root 运行则写入 `/etc/systemd/system/`：
```bash
# 1. 一键安装 systemd 服务
./bin/aew service install

# 2. 启动服务
./bin/aew service start

# 3. 查看状态
./bin/aew service status

# 4. 停止与卸载服务
./bin/aew service stop
./bin/aew service uninstall
```

**方式 2：手动创建 systemd 文件（可选）**：
创建系统服务文件 `/etc/systemd/system/aie-workstation.service`：
```ini
[Unit]
Description=AI Employee Platform Workstation Daemon
After=network.target

[Service]
Type=simple
User=your_username
Environment="AIE_DATA_DIR=/home/your_username/.aie"
ExecStart=/usr/local/bin/aew daemon
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```
加载并启动：
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now aie-workstation
sudo systemctl status aie-workstation
```

#### 选项 C：Windows 环境（系统服务与开机自启）

`aew` 在 Windows 平台上深度集成了原生 Windows 服务管理器（通过底层 `sc.exe`），无需第三方工具即可完成系统服务托管：

**方式 1：使用内置服务命令（推荐，需以“管理员身份运行” PowerShell 或 CMD）**：
```powershell
# 1. 注册为 Windows 系统服务（服务名：AIEmployeeWorkstation，启动类型：自动）
.\bin\aew.exe service install

# 2. 启动系统服务
.\bin\aew.exe service start

# 3. 查看服务运行状态
.\bin\aew.exe service status

# 4. 如需重启或停止服务：
.\bin\aew.exe service restart
.\bin\aew.exe service stop

# 5. 卸载服务：
.\bin\aew.exe service uninstall
```

**方式 2：使用 Windows 任务计划程序（非管理员/用户登录免权限自启）**：
若日常开发主机无 Administrator 提权权限，可使用 Windows 自带的任务计划程序实现静默自启：
1. 按 `Win + R` 键，输入 `taskschd.msc` 打开“任务计划程序”。
2. 点击右侧 **“创建任务”**：
   - **常规**：名称输入 `AI Employee Workstation`，勾选“使用最高权限运行”（如有）。
   - **触发器**：新建触发器，选择 **“登录时”**（当前用户登录时立即触发）。
   - **操作**：新建操作，选择“启动程序”：
     - 程序或脚本：`C:\path\to\aew.exe`（填写绝对路径）
     - 添加参数：`daemon`
     - 起始于：`C:\path\to\`（程序所在目录）
   - **条件**：取消勾选“只有在计算机使用交流电源时才启动此任务”。
3. 点击确定保存，注销或重启后工作站将自动在后台无窗口静默启动并重连中心。

---

## 五、端到端业务闭环验证

完成上述部署后，即可在管理后台验证全流程协同：

1. **工作站感知**：
   - 登录后台进入 **计算工作站 (Workstations)** 列表。
   - 您将看到刚接入的真实节点（如 `WS-6a11298e-...`），状态显示为带有脉冲光环的绿色 `ONLINE`，并呈现真实的 CPU、内存和磁盘百分比。
2. **工作区绑定 (Workspace)**：
   - 在后台「项目工作区」先**选择已接入的工作站节点**，再填入该节点本机的实际工程目录：
     - **Windows 示例**：`D:\Projects\my-project` 或 `C:\Users\username\Work\client-app`
     - **macOS / Linux 示例**：`/Users/username/Work/my-project`
   - 路径只在所选工作站上有效；执行任务时会把该 path 下发给对应节点。
3. **数字员工定义 (Employee)**：
   - 创建一名数字员工（如 `研发工程师 - 小艾`），将其工作站绑定为已接入的工作站节点，工作区绑定为上述代码工程。
4. **派发任务 (Job)**：
   - 在 **任务管理 (Jobs)** 中点击“手动派发新任务”，指定数字员工并输入指令（如 `检查当前工程代码规范并输出报告`）。
   - 中心控制面将基于资源感知限额调度器，通过 mTLS gRPC 长连安全下发任务至工作站执行。
   - Workstation 通过 Cursor CLI **`agent acp`**（stdio ACP）驱动 Agent，**不会打开 Cursor IDE GUI**。工作站需已安装 Cursor CLI（`agent`）并完成 `agent login`。

---

## 六、常见运维与故障排查 FAQ

### Q1: 工作站状态显示 `reconnect: CONNECTING` 或无法连接中心服务？
- **检查端口连通性**：工作站必须能够访问中心服务器的 `9090` 端口：
  - **macOS / Linux**：
    ```bash
    nc -zv <中心服务器IP> 9090
    # 或
    telnet <中心服务器IP> 9090
    ```
  - **Windows (PowerShell)**：
    ```powershell
    Test-NetConnection -ComputerName <中心服务器IP> -Port 9090
    ```
- **检查证书有效期与时间同步**：确保工作站与中心服务器系统时间保持 NTP 同步（时间偏差超过证书容差会导致 TLS 握手失败）。

### Q2: 如何安全吊销（Revoke）已废弃或失窃的工作站？
- 管理员可在管理后台“工作站管理”中选中目标工作站，点击 **“吊销证书 (Revoke Certificate)”**（需输入管理员密码进行二次高危鉴权 Step-Up）。
- 吊销后中心 CA 证书注销列表（CRL）将生效，中心控制面立即切断该节点的长连，并永久拒绝其重连。

### Q3: 如何备份与还原中心数据？
- 数据库位于 Docker volume `aie_pg_data` 中，可通过标准命令备份：
  ```bash
  docker exec -t aie-postgres pg_dumpall -c -U aie > aie_backup_$(date +%Y%m%d).sql
  ```
- 还原备份：
  ```bash
  cat aie_backup.sql | docker exec -i aie-postgres psql -U aie -d aie
  ```

### Q4: 重启容器后为什么不需要重新走 `/setup` 向导？
- 当首次部署在 `/setup` 创建首个管理员后，账号信息已安全写入持久化数据库，且向导通道已永久锁定。重启容器后服务检测到管理员已存在，会自动输出 `系统认证: 已就绪 (已存在管理员)` 并直接提供正常登录通道。
