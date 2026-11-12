# AI Employee Platform

由中心服务器（Control Plane）统一管理多个 AI Employee，并通过 Workstation 将 Agent 部署到物理计算机上的平台。

设计基线：[`docs/AI_Employee_System_完整技术设计与开发需求.md`](docs/AI_Employee_System_完整技术设计与开发需求.md)  
开发任务：[`docs/DEVELOPMENT_TASKS.md`](docs/DEVELOPMENT_TASKS.md)  
技术栈：[`docs/STACK.md`](docs/STACK.md)  
决策记录：[`docs/DECISIONS.md`](docs/DECISIONS.md)

## 仓库结构

```text
ai-employee-platform/
├── server/          # Control Plane（Go）
├── workstation/     # aew 守护进程与 CLI（Go）
├── admin/           # Admin 管理后台（React + TypeScript）
├── proto/           # gRPC / 协议唯一来源（M1）
├── migrations/      # PostgreSQL migrations（M1）
├── deploy/          # Docker Compose 等
├── docs/            # 设计与任务文档
├── scripts/         # 辅助脚本
└── Makefile
```

## 环境要求

| 组件 | 版本 |
|------|------|
| Go | 服务端 ≥ 1.25.0；工作站 ≥ 1.22.7（以各模块 `go.mod` 为准） |
| Node.js | ≥ 20 |
| PostgreSQL | 16（M1+，本地可用 Docker） |

V1 **不引入 Redis**。

## 构建

### Control Plane（server）

```bash
cd server
go mod tidy
go test ./...
go build -o bin/server ./cmd/server
go build -o bin/migrate ./cmd/migrate
```

或在仓库根目录：`make build-server`

### Workstation（aew）

```bash
cd workstation
go mod tidy
go test ./...
go build -o bin/aew ./cmd/aew
```

或：`make build-workstation`

### Admin

```bash
cd admin
npm install
npm run dev      # 开发
npm run build    # 生产构建
```

或：`make build-admin`

## 开发原则（摘要）

1. Control Plane 决定 What/Who/When；Workstation 决定 Where/How。
2. Job **不**直接调用 Cursor；经 Provider → ACP。
3. Proto 是 Server ↔ Workstation 协议唯一来源；禁止手写重复通信结构。
4. 禁止启动时偷偷改生产库 schema（必须用 migrations）。
5. 注释与公开 API 说明使用**中文**，便于团队阅读与扩展。

## 部署与快速上手

完整的生产与开发环境部署步骤，请参考：
👉 **[安装配置中心服务器与 Workstation 部署手册](docs/DEPLOYMENT_GUIDE.md)**

环境变量逐项说明、Windows PowerShell/CMD、macOS、Linux、群晖 DSM 的密码和密钥生成步骤：**[.env 配置与密钥生成手册](docs/ENV_CONFIGURATION.md)**。已有部署升级先阅读 **[安全加固与旧数据迁移](docs/SECURITY_HARDENING_2026-10-25.md)**，保留原密钥。

PVE 集群、Linux 虚拟机、软件部署与自动更新：**[PVE 与软件管理手册](docs/PVE_SOFTWARE_GUIDE.md)**。

### 容器启动（首次安装）

先准备可访问的 HTTPS 域名及证书入口。下面使用内置 Caddy 网关，要求宿主机的 80/443 可用；内网 IP、群晖反向代理、离线导入及已有部署升级见完整部署手册。已有部署保留现有 `.env` 和密钥，不要重新生成。

```bash
cd deploy

# 仅首次安装：生成独立随机密钥；替换为你的域名
sh ./generate-env.sh --domain employee.example.com \
  --server-image aie-control-plane:local \
  --admin-image aie-admin:local

# 从源码构建，并启用 HTTPS 网关（普通 up 不会启动 gateway）
docker compose -f docker-compose.yml -f docker-compose.build.yml --profile public up --build -d
```

上述是 macOS/Linux/群晖的系统 Shell 命令（需 OpenSSL）；Windows 使用 `generate-env.ps1 -Domain ... -ServerImage ... -AdminImage ...`，完整 PowerShell/CMD 命令见环境配置手册。首次配置无需安装 Python。

浏览器访问 `https://employee.example.com/setup`，输入 `.env` 中的 `AIE_SETUP_TOKEN` 创建管理员，然后前往审批中心完成 TOTP 绑定。远程工作站接入前，还需在 `.env` 中将 `AIE_GRPC_BIND_IP` 设为服务器的内网 IP，并在防火墙中允许工作站来源访问 gRPC 端口，再重建 `control-plane`。默认 HTTP、管理后台和 gRPC 宿主机端口只绑定回环，PostgreSQL 不发布宿主机端口。

### 工作站快速接入（以 macOS / Linux 为例）
```bash
# 1. 编译工作站程序
cd workstation && go build -o bin/aew ./cmd/aew

# 2. 在 Web 后台生成接入 Token 后注册工作站
AIE_DATA_DIR="$HOME/.aie" ./bin/aew register \
  --server https://employee.example.com \
  --token "<ENROLL_TOKEN>"

# 3. 启动常驻守护进程
AIE_DATA_DIR="$HOME/.aie" ./bin/aew daemon
```

注册令牌默认 15 分钟、最长 60 分钟、单次使用。远程注册强制 HTTPS，并校验证书；`register` 会自动保存 HTTP 地址和探测到的 gRPC 端口，不支持 `--grpc-target`。使用独立 gRPC 主机时，在注册后修改工作站 `config/config.yaml` 的 `control_plane.endpoint`。

## 当前进度

- **M0 已完成**（T-0101…T-0106）
- **M1 已完成**（T-0107…T-0121）：proto + gen、PostgreSQL migrations、SQLite schema、docker-compose
- **M2 已完成**（T-0201…T-0209）：Auth/RBAC、Enrollment、CA、mTLS gRPC、`aew register/ping`
- **M3 已完成**（T-0801…T-0808）：Heartbeat、ACK、Outbox、Resume、Reconnect、防 Replay
- **M4 已完成**（T-0301…T-0307, T-0401…T-0406）：业务对象 CRUD、Job/Session 状态机、Admin + SSE
- **M5 已完成**（T-0601…T-0608, T-0701…T-0705）：`aew daemon`/IPC/CLI/Service、Runtime、Provider/ACP
- **M6 已完成**（T-0501…T-0507）：Feishu Webhook、Secret 引用、Scheduler、Notification、Golden Path
  - 半自动步骤：[`docs/golden_path.md`](docs/golden_path.md)
  - 异常矩阵：[`docs/TEST_MATRIX_V1.md`](docs/TEST_MATRIX_V1.md)
- **M7 已完成**（T-0901…T-0907）：Permission Engine、Approval、TOTP、Admin step-up
  - 安全矩阵：[`docs/TEST_MATRIX_V2_SECURITY.md`](docs/TEST_MATRIX_V2_SECURITY.md)
- **M8 已完成**（T-0908…T-0912）：SecretManager、运行时注入、Admin Secrets、高级 Audit
  - Audit 策略：[`docs/AUDIT.md`](docs/AUDIT.md)
- **M9 / V2 已完成**（T-1001…T-1010）：Artifact、Provider Registry、Update、Metrics、doctor
  - 总验收：[`docs/TEST_MATRIX_V2.md`](docs/TEST_MATRIX_V2.md)
  - Q-06 已决 B（CP 下发签名公钥）
- 下一步：按需进入 **V3**（当前路线图暂缓）或运维硬化
- V3 暂缓
- **工作流MCP 已落地**：见 [`docs/WORKFLOW_MCP.md`](docs/WORKFLOW_MCP.md)（Go 重写 PersonalWorkMCP、工作流单点授权、Admin「工作流管理」页、ACP mcpServers 注入）


任务描述：
现在任务都是通过飞书发送的，现在我想在主分类“任务调度流转”下添加一个子页签“自动化任务”，这个“自动化任务”页签里主要包含：
1. 定时任务周期性的任务制定，比如每日，每周，每月，每年，自定义周期等
2. 日历任务，在日历的每一天上挂任务
3. webhook推送，接收外部的webhook推送，需要在这个页面进行webhook的配置，接收的webhook一定要做安全校验不然任何人都可以推送webhook.

要求：
1. 代码设计可扩展
2. 代码要有注释
3. 代码结构清晰
