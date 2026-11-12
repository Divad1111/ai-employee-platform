# AI Employee Platform 安装与部署手册

本手册详细指导如何在宿主机/服务器上部署 **中心服务器（Control Plane Stack）** 以及在开发机（Windows / macOS / Linux）上安装配置 **计算工作站（Workstation Daemon）**，实现端到端基于 mTLS 双向认证的安全协同。

本文以当前安全加固后的 Compose 与 CLI 为准。**全部 `.env` 字段、Windows PowerShell/CMD、macOS、Linux、群晖 DSM 的密码/密钥生成方法见 [.env 配置与密钥生成手册](ENV_CONFIGURATION.md)**；模板本身也附有用途和生成命令。已有部署升级前先阅读 [旧数据与密钥迁移说明](SECURITY_HARDENING_2026-10-25.md#已有数据先迁移密钥不能直接替换变量)，不能重新生成 `.env` 后直接覆盖旧配置。

---

## 目录
- [一、系统架构与组件拓扑](#一系统架构与组件拓扑)
- [二、环境前置准备与目录规划](#二环境前置准备与目录规划)
- [三、中心服务器安装与部署](#三中心服务器安装与部署)
  - [3.1 Docker 一键容器化部署（多架构 / 群晖 / 离线导入）](#31-容器化一键部署生产--群晖-dsm-推荐)
  - [3.2 首次部署向导（Setup Wizard）配置](#32-首次部署向导setup-wizard配置)
- [四、计算工作站（Workstation）安装与接入](#四计算工作站workstation安装与接入)
  - [4.1 获取并安装工作站 CLI/Daemon 程序（预编译包 / 源码）](#41-获取并安装工作站-clidaemon-程序)
  - [4.2 获取工作站接入令牌（Enrollment Token）](#42-获取工作站接入令牌enrollment-token)
  - [4.3 执行工作站身份注册与 mTLS 证书签发](#43-执行工作站身份注册与-mtls-证书签发)
  - [4.4 启动工作站守护进程并验证状态](#44-启动工作站守护进程并验证状态)
  - [4.5 配置工作站开机自启（macOS / Linux / Windows）](#45-配置工作站开机自启macos--linux--windows)
- [五、端到端业务闭环验证](#五端到端业务闭环验证)
- [六、常见运维与故障排查 FAQ（含数据备份与容灾恢复）](#六常见运维与故障排查-faq)

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
                           |                     |    - Nginx Web (:80)    |  |
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

管理页面、REST API、SSE 和 MCP 通过 HTTPS 网关进入；工作站的 mTLS gRPC 是独立直连入口。上图为容器内部端口，宿主机映射如下：

| 默认宿主机端口 | 协议 | 组件 | 用途说明 | 默认访问边界 |
| :--- | :--- | :--- | :--- | :--- |
| **80 / 443** | HTTP / HTTPS | 可选 Caddy gateway | 对外 Web/API/MCP 入口，80 用于跳转与证书验证 | 仅启用 `public` profile 后发布到所有网卡 |
| **8088** | HTTP | `aie-admin`，容器 80 | 管理后台与 `/api/` 转发 | 仅 `127.0.0.1`，可用 `AIE_ADMIN_PORT` 修改 |
| **8080** | HTTP | `aie-control-plane` | REST API、SSE、MCP | 仅 `127.0.0.1`，可用 `AIE_HTTP_PORT` 修改 |
| **9090** | mTLS gRPC | `aie-control-plane` | 工作站长连接 | 默认仅 `127.0.0.1`，远程接入需显式修改绑定地址 |
| 不发布 | PGSQL，容器 **5432** | `aie-postgres` | PostgreSQL 主数据库 | 仅容器内网，无默认宿主机 5432 映射 |

---

## 二、环境前置准备与目录规划

#### 2.1 中心服务器宿主机
- **操作系统**：Linux（Ubuntu 22.04+ / Debian 12+ / CentOS 8+）或 macOS（13.0+）
- **软件依赖**：
  - Docker 24.0+
  - Docker Compose v2.20+
  - （如需源码编译）服务端 Go 1.25.0+，Node.js 20+

#### 2.2 计算工作站主机
- **操作系统**：
  - **Windows**（Windows 10 / 11 64位，Windows Server 2019+）
  - **macOS**（Apple Silicon M系列 / Intel 13.0+）
  - **Linux**（Ubuntu 20.04+、Debian 11+、CentOS/RHEL 8+ 等 x86_64 / aarch64）
- **软件依赖**：
  - Go 1.22.7+（用于工作站源码编译，以模块 `go.mod` 为准；预编译包无需 Go）
  - Git（代码版本管理与工作区交互，Windows 请安装 Git for Windows）
  - 命令行终端：PowerShell 5.1+ / 7+（Windows 推荐），或 Bash / Zsh（macOS / Linux）
  - （可选）日常 IDE：Cursor / VS Code
  - **（执行 Agent 必需）** Cursor CLI：`agent` 命令可用（`~/.local/bin/agent`），用于 `agent acp` 无头 ACP，而非打开 Cursor.app

#### 2.3 部署目录结构与权限规划清单

在开始部署前，请了解中心服务器与工作站各需要创建哪些目录：

##### A. 中心服务器（Control Plane Stack）
| 部署方式 | 目录路径 | 属主与建议权限 | 用途说明 |
| :--- | :--- | :--- | :--- |
| **Docker Compose（推荐）** | `/opt/ai-employee-platform` (或任意部署目录) | `0755` | 存放项目配置文件、`docker-compose.yml` 及 `.env` 变量 |
| **Postgres 数据库数据** | `./data/postgres` (容器内 `/var/lib/postgresql/data`) | 映射宿主机目录 `0700` | PostgreSQL 主数据库全量数据，镜像更新与重启绝不丢失 |
| **CA 根证书与私钥** | `./data/server/ca` (容器内 `/data/ca`) | 容器用户 `0700` | mTLS 根证书体系，持久化确保证书在服务升级后长期有效 |
| **机密凭证落盘** | `./data/server/secrets` (容器内 `/data/secrets`) | 容器用户 `0700` | 经由 AES-256-GCM 加密的安全保险库持久化目录 |
| **系统数据备份与应急快照** | `./data/server/backups` (容器内 `/data/backups`) | 容器用户 `0750` | 存放全量数据备份快照与容灾还原前自动生成的 Emergency 快照 |
| **任务产物与归档** | `./data/server/artifacts` (容器内 `/data/artifacts`) | 容器用户 `0750` | 存放数字员工任务执行生成的代码产物与归档文件 |

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

### 3.1 容器化一键部署（生产 / 群晖 DSM 推荐）

平台支持**直接通过发布的多架构容器镜像部署**，无需本地安装 Go / Node 构建环境，原生兼容 Linux x86_64、Linux ARM64、群晖 DSM NAS、macOS (Docker Desktop / OrbStack) 与 Windows (Docker Desktop / WSL2)。

* **官方在线镜像地址 (GHCR)**：
  - 管控面与数据库迁移：`ghcr.io/divad1111/ai-employee-platform/control-plane:<version>`
  - 前端管理后台 (Web)：`ghcr.io/divad1111/ai-employee-platform/admin:<version>`

选择实际已发布且核验过的版本或 digest，生产部署不依赖可变的 `latest` 自动升级。

---

#### 所有部署方式共用的配置前提

部署目录需要 `docker-compose.yml`、私密 `.env`，使用内置网关还需同目录的 `Caddyfile`。首次安装按 [.env 手册](ENV_CONFIGURATION.md) 使用原生生成器：Windows 为 `generate-env.ps1`，macOS/Linux/群晖为 `generate-env.sh`（需 OpenSSL），均与 `.env.example` 同目录，不要求 Python。也可手动填写模板，不能把 `CHANGE_ME` 当作生产配置。

生产数据库密码至少 20 字符；`AIE_SECRET_MASTER_KEY`、`AIE_BACKUP_MASTER_KEY`、`AIE_SETUP_TOKEN` 各至少 32 字符且互不复用。首次初始化需要 `AIE_SETUP_TOKEN`，管理员需要绑定 TOTP。模板中的业务镜像是本地构建标签，在线/离线部署必须与实际镜像引用一致。

远程注册强制可信 HTTPS。`AIE_PUBLIC_SERVER_URL` 只声明客户端使用的完整地址，**不会启动 HTTPS 监听，也不会签发证书**。修改 `AIE_HTTP_PORT=9080` / `AIE_ADMIN_PORT=9088` 后，这两个端口仍是回环 HTTP 后端；`https://192.168.4.243` 仍指向 443。使用非默认 HTTPS 端口时，地址和网关实际监听要同时修改。

HTTPS 入口按实际环境选择一种：

- **内置 Caddy**：配置 `AIE_DOMAIN` 和 `AIE_PUBLIC_SERVER_URL`，确认宿主机 80/443 未占用，使用 `--profile public` 启动。普通 `docker compose up -d` 不会启动 gateway。公网域名需满足 DNS 和证书验证条件；内网 IP 的本地 CA 信任步骤见下文。
- **已有 HTTPS 代理**：不启动 gateway。`/api/*` 和 `/mcp` 转发到 `127.0.0.1:<AIE_HTTP_PORT>`，其他路径转发到 `127.0.0.1:<AIE_ADMIN_PORT>`。保留 Host、正确覆盖转发头，按控制面实际收到的代理源地址设置 `AIE_TRUSTED_PROXIES`；不能信任任意来源。不同宿主机的代理无法直接访问这些回环端口，需另行设计受限内网连接。

群晖 DSM 已占用 80/443 时不要同时启动占用这些端口的 gateway。若仅将整站代理到 Admin 端口，管理页面与 `/api/` 可用，但当前 Admin Nginx 不转发 `/mcp`；完整平台入口需按上述路径路由，或调整端口方案后使用内置网关。

远程工作站还需要独立的 gRPC 入口。将默认 `AIE_GRPC_BIND_IP=127.0.0.1` 改成服务器内网 IP，例如 `192.168.4.243`，并限制防火墙仅允许工作站来源访问 `AIE_GRPC_PORT`。HTTP 网关不代理 gRPC。

#### 方式 A：标准宿主机在线拉取镜像部署（通用推荐）

在 `deploy` 目录生成新安装配置，替换域名及实际已发布版本（Windows/DSM 等其他平台命令见 `.env` 手册）：

```bash
cd deploy
AIE_VERSION='vX.Y.Z'
sh ./generate-env.sh --domain employee.example.com \
  --server-image "ghcr.io/divad1111/ai-employee-platform/control-plane:${AIE_VERSION}" \
  --admin-image "ghcr.io/divad1111/ai-employee-platform/admin:${AIE_VERSION}"

# 按需修改 .env 中的目录、gRPC 绑定地址及其他配置后检查并启动
docker compose --profile public config --quiet
docker compose --profile public pull
docker compose --profile public up -d
docker compose --profile public ps -a
```

使用已有 HTTPS 代理时省略 `--profile public`。数据库和服务目录分别持久化到 `AIE_PG_DATA_DIR` / `AIE_SERVER_DATA_DIR`；Caddy 证书和本地 CA 在 `gateway_data` 卷中，需一并保存。`migrate` 会执行增量 schema 迁移，不能替代旧密钥迁移或数据库改密。

---

#### 方式 B：下载 GitHub Releases 离线镜像包（无外网服务器 / 群晖 DSM 导入推荐）
若服务器处于受限内网环境，或希望在群晖 NAS 上跳过外网拉取过程，可直接在 GitHub Releases 资产列表中下载离线镜像压缩包：
* **x86_64 (amd64) 架构**：
  - `control-plane-dsm-<version>-amd64.tar.gz`
  - `admin-dsm-<version>-amd64.tar.gz`
* **ARM64 / aarch64 架构**（如 ARM 架构群晖 NAS、树莓派等）：
  - `control-plane-dsm-<version>-arm64.tar.gz`
  - `admin-dsm-<version>-arm64.tar.gz`

**导入与启动方法**：
1. **命令行导入**：
   ```bash
   # Docker 直接读取 gzip 归档；每个通配符只能匹配一个文件
   docker load -i control-plane-dsm-*-amd64.tar.gz
   docker load -i admin-dsm-*-amd64.tar.gz
   ```
   ARM64 使用对应 `arm64` 包。Docker 自动解压，无需 `gzip -dc`；参见 [Docker 官方 load 文档](https://docs.docker.com/reference/cli/docker/image/load/)。目录有多个版本时填写完整文件名，或逐文件循环导入；不能向一次 `docker load -i` 传入多个归档。Windows PowerShell 使用实际完整文件名。

   先核验 Release 提供的 `SHA256SUMS.txt`：Linux/DSM 有 `sha256sum` 时可用 `sha256sum <归档文件名>`，macOS 用 `shasum -a 256 <归档文件名>`，PowerShell 用 `Get-FileHash -Algorithm SHA256 <归档文件名>`，与该文件对应条目比较。
2. **群晖 DSM Container Manager 界面导入**：
   - 打开 DSM 的 **Container Manager** -> 点击左侧 **「映像」**。
   - 点击顶部 **「新增」** -> **「从文件添加」**。
   - 分别选取下载的两个 `.tar.gz` 压缩包上传，群晖将自动解包并注册到本地镜像列表中。
3. **匹配本地镜像标签**：

   当前打包流程保存的镜像标签是 `aie-control-plane:latest` 与 `aie-admin:latest`；归档文件名中的版本不会自动变成镜像标签。导入后根据 `docker load` 输出确认标签，重新打版本标签，并在首次生成 `.env` 时使用相同引用：

   ```bash
   AIE_VERSION='vX.Y.Z'
   AIE_ARCH='amd64'
   docker tag aie-control-plane:latest "aie-control-plane:${AIE_VERSION}-${AIE_ARCH}"
   docker tag aie-admin:latest "aie-admin:${AIE_VERSION}-${AIE_ARCH}"
   sh ./generate-env.sh --domain employee.example.com \
     --server-image "aie-control-plane:${AIE_VERSION}-${AIE_ARCH}" \
     --admin-image "aie-admin:${AIE_VERSION}-${AIE_ARCH}"
   ```

   已有 `.env` 时保留密钥，仅更新两项镜像引用，不运行生成器。ARM64 同步更改 `AIE_ARCH`。

4. **准备依赖镜像**：

   两份业务包不包含 PostgreSQL、BusyBox 或 Caddy。完全离线时，在可联网机器上拉取目标架构依赖后导出，传到服务器导入；以下与模板默认镜像一致：

   ```bash
   # 可联网机器（ARM64 将 linux/amd64 替换为 linux/arm64）
   docker pull --platform linux/amd64 postgres:16-alpine
   docker pull --platform linux/amd64 busybox:1.37.0
   docker pull --platform linux/amd64 caddy:2-alpine
   docker save -o aie-dependencies-amd64.tar postgres:16-alpine busybox:1.37.0 caddy:2-alpine

   # 离线服务器：先传入上面生成的 tar
   docker load -i aie-dependencies-amd64.tar
   ```

   使用已有代理时无需 Caddy。依赖版本如有调整，导入标签与 `.env` 必须同步。

5. **启动**：完成配置和 HTTPS 信任准备后，在 `deploy` 目录执行：

   ```bash
   docker compose --profile public up -d --pull never
   docker compose --profile public ps -a
   ```

   使用已有代理时省略 profile。没有对应依赖镜像时 `--pull never` 会明确失败，不会尝试访问外网。

---

#### 方式 C：群晖 DSM (Container Manager) 在线项目部署
1. 打开群晖 DSM，进入 **Container Manager**。
2. 进入 **「项目 (Project)」** -> 点击 **「新增」**。
3. 设定项目名称（如 `ai-employee`），路径选择 NAS 共享文件夹（例如 `/volume1/docker/ai-employee`）。
4. 在项目目录保存仓库中的 `docker-compose.yml`，使用内置网关时还需 `Caddyfile`。
5. **必须准备有效的私密 `.env`**，按 `.env` 手册生成随机密码、密钥并指定实际镜像/HTTPS 地址，同时可指定群晖数据绝对路径：
   ```env
   AIE_PG_DATA_DIR=/volume1/docker/ai-employee/data/postgres
   AIE_SERVER_DATA_DIR=/volume1/docker/ai-employee/data/server
   ```
6. 使用已有 HTTPS 代理时可从项目界面启动；使用内置 gateway 且界面未启用 profile 时，通过 SSH 进入该目录执行 `docker compose --profile public up -d`，否则 443 没有该网关监听。
7. 升级前备份数据、`.env` 和网关卷，更新为核验后的镜像版本再拉取重建。已有管理员和数据保留，但安全加固前的旧密文需先离线迁移，不能直接套用新密钥。

---

#### 方式 D：本地源码编译构建部署（开发调试）

首次安装先用生成器填写自己的域名和本地构建标签，例如 `--server-image aie-control-plane:local --admin-image aie-admin:local`。保留生产安全配置，使用构建覆盖文件：

```bash
cd deploy
docker compose -f docker-compose.yml -f docker-compose.build.yml --profile public up --build -d
```

使用已有 HTTPS 代理时省略 profile。不要通过关闭生产校验来绕过 HTTPS 或密钥要求。

#### 内网 IP HTTPS：以 `192.168.4.243` 为例

网络配置片段如下，仍需保留 `.env` 中的镜像、数据库及密钥配置：

```env
AIE_HTTP_PORT=9080
AIE_ADMIN_PORT=9088
AIE_GRPC_PORT=9090
AIE_GRPC_BIND_IP=192.168.4.243
AIE_PUBLIC_SERVER_URL=https://192.168.4.243
AIE_DOMAIN=192.168.4.243
```

启用内置 gateway 后，Caddy 为内网 IP 使用本地 CA。容器内生成的 CA 不会自动被其他机器信任，需从服务器导出**公有根证书**，通过可信渠道传给各工作站：

```bash
docker compose --profile public up -d
docker compose cp gateway:/data/caddy/pki/authorities/local/root.crt ./aie-https-root.crt
```

安装前通过可信渠道核对证书指纹，不要复制 CA 私钥；此 HTTPS CA 与工作站 mTLS CA 是两套身份材料。客户端安装方式：

- **Windows PowerShell（管理员）**：`Import-Certificate -FilePath .\aie-https-root.crt -CertStoreLocation Cert:\LocalMachine\Root`，覆盖系统服务所需的机器证书存储。
- **Windows CMD（管理员）**：`certutil -addstore -f Root aie-https-root.crt`。
- **macOS**：`sudo security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain ./aie-https-root.crt`。
- **Linux Debian/Ubuntu**：`sudo cp aie-https-root.crt /usr/local/share/ca-certificates/aie-https-root.crt`，再执行 `sudo update-ca-certificates`；RHEL 系用系统对应的 `update-ca-trust` 流程。
- **群晖 DSM 作为客户端**：向实际运行客户端的系统/容器信任库导入该根证书；DSM 用于提供站点证书的入口不等于 `aew` 客户端信任库。若 NAS 仅作为服务器，需安装根证书的是连接它的各工作站。

浏览器如使用独立信任库，还需导入该 CA；网页点击“忽略证书错误”不会让 `aew` 信任证书。安装后在工作站访问 `https://192.168.4.243/api/enrollment/endpoint`，应得到 JSON 且没有证书错误。操作依据见 [Caddy 本地 HTTPS 文档](https://caddyserver.com/docs/running#local-https-with-docker)。

---

### 3.2 首次部署向导（Setup Wizard）配置

中心服务器启动后，为了保证安全性，系统**不包含任何硬编码管理员账号**，必须通过首次部署向导进行初始化：

1. 打开浏览器访问控制台：
   ```
   https://employee.example.com/
   ```
2. 系统会自动检测到未初始化状态，并无缝重定向至首次部署向导：
   ```
   https://employee.example.com/setup
   ```
3. 在页面中填写初始超级管理员配置：
   - **部署初始化密钥**：填入服务端 `.env` 中的 `AIE_SETUP_TOKEN`，不是工作站接入 Token。
   - **平台/实例名称**：例如 `AI Employee 数字员工管控中心`
   - **超级管理员账号**：输入自定义账号（例如 `admin` 或企业管理主账号）
   - **管理员昵称**：例如 `系统管理员`
   - **管理员登录密码**：设置安全密码（至少 8 位，包含字符与数字）
   - **确认登录密码**：再次确认密码
4. 点击 **“完成初始化并进入控制台”**。
   - 系统将创建超级管理员，自动保存登录凭据并进入主控制台。
   - **生产管理员 MFA**：先前往审批中心完成 TOTP 绑定与确认，之后登录须提交当前动态验证码；未完成前其他受保护 API 会拒绝访问。
   - **安全锁定保护**：初始化接口（`/api/setup/init`）完成初始化后将**永久关闭并锁定**，后续所有重入请求均返回 `409 Conflict`，杜绝任何外部攻击者重置或提权可能。

上述地址替换成 `AIE_PUBLIC_SERVER_URL` 的实际值；内网 IP 部署须先完成证书信任。数据库 schema 自动迁移不等于自动设置管理员或完成 TOTP。

---

## 四、计算工作站（Workstation）安装与接入

计算工作站作为实际承载研发任务和本地 IDE / Agent 操作的计算节点，通过双向 mTLS 证书安全接入中心控制面。

### 4.1 获取并安装工作站 CLI/Daemon 程序

#### 方式 A：直接从 GitHub Releases 下载预编译文件（推荐，无需 Go 环境）

工作站端程序（`aew`）已在项目的 GitHub Releases 中提供全平台的预编译独立单文件，无需安装配置 Go 编译环境，直接下载对应系统架构的文件即可开箱使用：

| 操作系统与处理器架构 | GitHub Releases 对应产物文件名 | 本地重命名目标 |
| :--- | :--- | :--- |
| **Windows (x64 / Intel / AMD)** | `aew-<version>-windows-amd64.exe` | `aew.exe` |
| **Windows (ARM64 / 骁龙平台)** | `aew-<version>-windows-arm64.exe` | `aew.exe` |
| **macOS (Apple Silicon M1/M2/M3/M4)** | `aew-<version>-darwin-arm64` | `aew` |
| **macOS (Intel 处理器)** | `aew-<version>-darwin-amd64` | `aew` |
| **Linux (x86_64 / amd64)** | `aew-<version>-linux-amd64` | `aew` |
| **Linux (ARM64 / aarch64)** | `aew-<version>-linux-arm64` | `aew` |

**快速初始化与全局命令链接**：
1. **macOS / Linux 环境**：
   ```bash
   # 1. 赋予执行权限并重命名
   chmod +x aew-*-darwin-arm64
   mv aew-*-darwin-arm64 aew
   
   # 2. 建立系统全局软链接（自动识别 /usr/local/bin 或 ~/.local/bin）
   ./aew link
   ```
2. **Windows 环境（PowerShell）**：
   ```powershell
   # 1. 重命名为 aew.exe
   Rename-Item -Path "aew-*-windows-amd64.exe" -NewName "aew.exe"
   
   # 2. 建立系统 PATH 全局软链接
   .\aew.exe link
   ```
执行 `link` 后，无论在哪个工作目录下均可直接输入 `aew` 运行！

---

#### 方式 B：本地源码自主构建（开发人员）

若希望直接从源码编译最新代码，进入 `workstation` 目录执行编译：

* **Windows 环境（PowerShell）**：
  ```powershell
  cd workstation
  go build -o bin\aew.exe .\cmd\aew
  .\bin\aew.exe link
  ```
* **macOS / Linux 环境（Bash / Zsh）**：
  ```bash
  cd workstation
  go build -o bin/aew ./cmd/aew
  ./bin/aew link
  ```
* **跨平台交叉编译（在任意 Linux/Mac 机器上为 Windows 构建 `aew.exe`）**：
  ```bash
  cd workstation
  CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/aew.exe ./cmd/aew
  ```

---

### 4.2 获取工作站接入令牌（Enrollment Token）

接入工作站需要向中心服务器申请一次性安全注册令牌（Token）。

**方式一：在 Admin Web 后台图形化一键生成（强烈推荐）**：
1. 使用已完成 TOTP 的管理员登录实际 HTTPS 管理后台，例如 `https://employee.example.com`。
2. 进入 **「工作站节点」**（页面 URL：`https://employee.example.com/workstations`）。
3. 点击页面右上角的绿色按钮 **「➕ 接入新工作站」**（若当前没有任何节点，也可直接点击页面正中央的“立即接入第一台工作站”按钮）。
4. 在展开的接入向导中：
   - 输入节点备注名称（如 `我的办公机-Mac` 或 `开发计算节点-Windows`）。
   - 设置令牌有效期（默认 **15 分钟**，最长 **60 分钟**，单次使用）。
   - 确认生成命令中的地址与 `AIE_PUBLIC_SERVER_URL` 一致，并且是实际可用的可信 HTTPS 入口。
5. 点击 **「生成一次性接入令牌」**。
   - 页面将直接生成单次有效的唯一 Token。
   - 页面下方将自动生成适用于 **macOS / Linux** 与 **Windows** 的完整一键注册命令，支持一键复制代码直接到终端中粘贴运行！

**方式二：通过 API 命令行直接生成**：
```bash
# 已在后台完成 TOTP 绑定；替换为实际 HTTPS 地址及当前登录凭据
AIE_SERVER_URL='https://employee.example.com'

# 1. 登录并提交当前 TOTP，获取已通过 MFA 的会话
ADMIN_TOKEN=$(curl -fsS -X POST "$AIE_SERVER_URL/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"<你的管理员账号>","password":"<你的管理员密码>","totp":"<当前6位动态验证码>"}' | jq -er .token)

# 2. 生成工作站 Enrollment Token
ENROLL_TOKEN=$(curl -fsS -X POST "$AIE_SERVER_URL/api/enrollment/tokens" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"label":"macOS-Dev-Node","ttl_minutes":15}' | jq -er .token)

echo "注册令牌: $ENROLL_TOKEN"
```

---

### 4.3 执行工作站身份注册与 mTLS 证书签发

在工作站主机上执行 `register`，申请客户端证书与唯一 Workstation ID。以下以源码构建的 `bin/aew` 为例；预编译包已执行 `link` 时可直接使用 `aew`。

远程注册强制 HTTPS，并校验系统信任链和域名/IP；只有字面回环地址（如 `http://127.0.0.1:8080`）允许 HTTP。必须直接填写最终 HTTPS 入口，注册客户端不跟随重定向。`register` 当前仅支持 `--server`、`--token`，**不再支持 `--grpc-target`**。

**macOS / Linux 环境**：
```bash
# 环境变量指定数据保存目录，默认建议使用 ~/.aie
export AIE_DATA_DIR="$HOME/.aie"

# 执行配对注册
./bin/aew register \
  --server https://employee.example.com \
  --token "<你的ENROLL_TOKEN>"
```

**Windows 环境（PowerShell）**：
```powershell
# 环境变量指定数据保存目录，默认建议使用 $env:USERPROFILE\.aie
$env:AIE_DATA_DIR = "$env:USERPROFILE\.aie"

# 执行配对注册
.\bin\aew.exe register `
  --server https://employee.example.com `
  --token "<你的ENROLL_TOKEN>"
```

**Windows 环境（CMD）**：
```cmd
set AIE_DATA_DIR=%USERPROFILE%\.aie
.\bin\aew.exe register --server https://employee.example.com --token "<你的ENROLL_TOKEN>"
```

**注册成功输出示例**：
```
注册成功
Workstation ID : WS-6a11298e-b404-4b6a-8a33-0de769999877
Identity Dir   : /Users/username/.aie/identity
Control Plane  : gRPC=employee.example.com:9090, HTTP=https://employee.example.com (已更新至 /Users/username/.aie/config/config.yaml)
私钥仅保存在本地，未上传 Control Plane。
```

注册完成后，工作站的身份文件已安全落盘在 `~/.aie/identity/`（Windows 为 `%USERPROFILE%\.aie\identity\`）：
- `ca.crt`：中心 CA 根证书
- `client.crt`：中心 CA 签发的工作站客户端证书（CN 包含工作站 ID）
- `client.key`：工作站本地生成的私钥（不离开本机）
- `workstation-id`：节点标识

注册会将 HTTP 地址及探测到的 gRPC 主机/端口写入 `config/config.yaml`。服务端通过 `AIE_GRPC_PORT` 声明端口；若 HTTPS 和 gRPC 使用不同主机地址，或无法探测端口，检查并手动调整 `control_plane.endpoint`，例如 `192.168.4.243:9090`，保留 `control_plane.http_endpoint` 为实际 HTTPS 地址，再启动/重启 daemon。注册成功不代表远程 gRPC 端口已开放。

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

- **检查绑定地址**：默认 `AIE_GRPC_BIND_IP=127.0.0.1` 仅允许服务器本机连接；远程工作站需改为服务器内网 IP，并执行 `docker compose up -d control-plane` 重建，再限制防火墙允许来源。
- **检查工作站配置**：`config/config.yaml` 的 `control_plane.endpoint` 应为工作站可达的主机与 gRPC 端口；不要填 HTTPS URL。HTTPS gateway 不代理 gRPC。
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

### Q3: 如何备份与容灾还原中心服务器数据？
- **方式一：通过 Admin Web 内置备份与容灾恢复系统（强烈推荐，图形化开箱即用）**：
  登录实际 HTTPS 管理后台，进入左侧导航 **「安全治理与系统」->「备份与容灾恢复」**（例如 `https://employee.example.com/backups`）：
  1. **全量资产范围**：全自动打包 PostgreSQL 关系数据库、mTLS CA 根证书私钥、AES 加密机密保险库、任务产物等中心全量资产（CENTER_FULL）。
  2. **多目标分发与保留策略**：支持配置每日/每周定时策略（Cron），保留最新 N 份自动淘汰；支持本地目录（Local）、对象存储（S3/MinIO）、SFTP 以及企业 NAS (SMB/CIFS)。
  3. **加密与压缩可选**：支持启用工业级 AES-256-GCM 认证加密，亦可根据需要配置未加密透明归档（采用 Zstandard 无损压缩）。
  4. **完整性防篡改校验**：随时针对历史快照执行 SHA-256 流式校验与 Manifest 清单核验。
  5. **安全容灾还原与前置应急保护**：
     - 还原高危操作由 Step-Up 二次管理员密码鉴权强力防护；
     - **全自动应急备份**：在覆盖当前数据前，系统会自动打一份当前现场状态的 **Emergency Backup 应急快照**并入库保护。若还原发生意外随时可在恢复历史中点击「查看应急快照」执行二次回滚！
- **方式二：宿主机物理文件备份（底层兜底）**：
  由于全量数据均已持久化映射到宿主机本地目录 `./data/`：
  - **整机冷备份**：停止容器后直接打包宿主机 `./data` 目录：
    ```bash
    tar -czvf aie_cold_backup_$(date +%Y%m%d).tar.gz ./data
    ```
  - **数据库热转储**：
    ```bash
    # 在 deploy 目录执行，使用容器中实际配置的数据库用户名
    docker compose exec -T postgres sh -c 'pg_dumpall -c -U "$POSTGRES_USER"' > aie_db_$(date +%Y%m%d).sql
    ```

  冷备份还需单独保存 `.env`、自定义数据目录和 `gateway_data` / `gateway_config` 卷；数据库转储不包含 Vault、CA 或备份主密钥，不能单独完成全部恢复。不要用新的随机主密钥启动旧数据。已有部署安全加固升级按专门迁移文档处理，不是直接“拉镜像并重启”。

### Q4: 重启容器后为什么不需要重新走 `/setup` 向导？
- 当首次部署在 `/setup` 创建首个管理员后，账号信息已安全写入持久化数据库，且向导通道已永久锁定。重启容器后服务检测到管理员已存在，会自动输出 `系统认证: 已就绪 (已存在管理员)` 并直接提供正常登录通道。

### Q5: `aew register` 报 443 连接被拒绝、证书错误或 HTTPS 要求？

- **`connectex ... actively refused` / `connection refused`**：尚未进入 Token 校验，先确认实际 HTTPS 主机/端口。`AIE_PUBLIC_SERVER_URL` 不创建监听；内置 gateway 需 `--profile public`，检查 `docker compose --profile public ps -a`、`docker compose logs --tail=100 gateway control-plane` 及宿主机端口占用/防火墙。
- **`x509: certificate signed by unknown authority`**：安装网关 CA 到实际运行 `aew` 的用户/系统信任库；作为系统服务运行时也需覆盖服务账户。不要关闭证书验证。
- **证书域名/IP 不匹配**：使用证书覆盖的域名，或为实际 IP 配置包含 IP SAN 的证书。只在浏览器忽略警告不会修复 CLI。
- **远程注册必须 HTTPS**：不要将远程命令改为 HTTP 9080；配置真实 HTTPS 入口。`--server` 使用最终地址，不能依赖跳转。
- **注册令牌过期/已使用**：连通 HTTPS 后重新生成 15 分钟的一次性令牌，不能用 `AIE_SETUP_TOKEN` 代替。

### Q6: 修改 `.env` 后没有生效，或者生产启动提示弱密钥？

检查终端同名环境变量是否覆盖 `.env`，确认命令读取的是正确的环境文件，并执行 `docker compose up -d` 重建相关服务；仅 `restart` 不加载新环境。首次安装按 [.env 手册](ENV_CONFIGURATION.md) 生成独立随机值。已有部署不能为消除启动错误直接替换主密钥，先备份并按旧数据迁移流程处理。
