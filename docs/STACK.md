# 技术栈与版本基线

> 对应任务：T-0104。变更版本时同步更新本文与 README。

## 语言与运行时

| 组件 | 版本 | 说明 |
|------|------|------|
| Go | **1.22+**（go.mod 锁定 `go 1.22`） | Control Plane + Workstation |
| Node.js | **20+** | Admin 构建 |
| TypeScript | **~5.7** | Admin |
| React | **19.x** | Admin |
| Vite | **6.x** | Admin 构建工具 |

## 数据与中间件

| 组件 | V1 | 说明 |
|------|----|------|
| PostgreSQL | **16** | Control Plane 主库 |
| SQLite | 随 Go 驱动 | Workstation 本地状态 |
| **Redis** | **不引入** | V1 明确不做（设计文档 §71、§85） |
| NATS/Kafka | 不引入 | V3 暂缓 |

## 通信

| 组件 | 选型 |
|------|------|
| Control Plane ↔ Workstation | gRPC + TLS 1.3 + mTLS |
| Admin ↔ Control Plane | REST + SSE（V1） |
| 协议定义 | `proto/aie/v1` + **buf** |
| 生成代码 | `gen/go/aie/v1`（`make proto`） |
| Proto Go 插件 | `protoc-gen-go` / `protoc-gen-go-grpc` |
| 可靠通信约定 | [`docs/RELIABILITY.md`](./RELIABILITY.md)（序号/ACK/乱序/Resume/Offline） |

## 迁移工具

| 组件 | 选型 |
|------|------|
| PostgreSQL migrations | **pressly/goose v3**（`server/cmd/migrate`） |
| Workstation SQLite schema | embed `store/schema.sql`（幂等执行） |

## 部署

| 组件 | 选型 |
|------|------|
| 容器 | Docker + Docker Compose |
| 反向代理 | Caddy 或 Nginx（按环境任选） |
| Compose 服务（V1） | `control-plane`、`postgres`、`admin-web`（无 Redis） |

## 配置策略

遵循设计文档 §80：

```text
config/default.yaml          # 默认，不直接改生产
+ config/<env>.yaml          # 覆盖层
→ Effective Config
```

路径约定（建议）：

| 模块 | 默认配置 | 覆盖配置 |
|------|----------|----------|
| server | `server/configs/default.yaml` | `server/configs/production.yaml` 或环境变量 `AIE_*` |
| workstation | 平台 ConfigDir 下 `config.yaml` | 同目录用户覆盖文件 |

敏感信息（密钥、Token）**不进 YAML**，走 Secret Manager / 环境变量 / OS 凭据库。

## 模块边界（扩展性）

```text
Admin → REST → server/internal/<domain> → database → PostgreSQL
Workstation CLI → IPC → Daemon → providers → ACP → Agent
JobManager 只依赖 AgentSession / AgentProvider 接口，不依赖具体 Cursor 类型
```

## 注释约定

- 文件头、导出类型/函数使用**中文注释**说明职责与设计依据章节。
- 协议字段可保留英文标识符，旁注中文语义。
