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
| Go | ≥ 1.22（本机建议与 CI 一致） |
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

### 容器一键启动
```bash
# 启动中心服务器栈（PostgreSQL + Control Plane + Admin Web）
cd deploy
docker compose up --build -d

# 浏览器访问首次部署向导，配置初始超级管理员
open http://localhost:8088/setup
```

### 工作站快速接入（以 macOS / Linux 为例）
```bash
# 1. 编译工作站程序
cd workstation && go build -o bin/aew ./cmd/aew

# 2. 在 Web 后台生成接入 Token 后注册工作站
AIE_DATA_DIR="$HOME/.aie" ./bin/aew register \
  --server http://127.0.0.1:8080 \
  --token "<ENROLL_TOKEN>" \
  --grpc-target "127.0.0.1:9090"

# 3. 启动常驻守护进程
AIE_DATA_DIR="$HOME/.aie" ./bin/aew daemon
```

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

