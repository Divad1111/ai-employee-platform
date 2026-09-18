# M1 契约冻结检查清单

> 对应任务：T-0121。全部勾选后视为 M1 完成，可进入 M2 Identity。

## Proto（协议唯一来源）

- [x] `proto/aie/v1/*.proto` 覆盖 common / workstation / heartbeat / command / event / job / session / worker_service
- [x] `make proto`（`cd proto && buf generate`）可重复生成
- [x] 生成物位于 `gen/go/aie/v1/`，server / workstation 经 `replace` 依赖，**禁止**手写重复通信结构
- [x] Workstation **出站**双向流：`WorkerService.Connect` + `Ping`
- [x] Command 含 command_id / sequence / timestamp / nonce；8 种业务命令类型齐全
- [x] Job / Session 状态枚举对齐设计文档

## PostgreSQL Migrations

- [x] `migrations/000001`…`000005`（goose Up/Down）
- [x] `server/cmd/migrate` 使用 goose，**禁止**应用启动自动改 schema
- [ ] 本机 `docker compose up postgres` + `make migrate-up` 实测（需 Docker Desktop 守护进程）

## Workstation SQLite

- [x] `workstation/internal/store/schema.sql` 含 §25 表与 outbox / server_commands sequence+acked
- [x] `sessions` 字段对齐 §53
- [x] schema 内容测试 `TestSchemaContainsRequiredTables` 通过
- [x] 运行时 `Open` 需注册 SQLite 驱动（如 `modernc.org/sqlite`）；M1 测试不强制下载驱动

## Docker / 本地环境

- [x] `deploy/docker-compose.yml`：postgres + control-plane + admin-web，**无 Redis**
- [x] `deploy/Dockerfile.server` / `Dockerfile.admin` / `nginx.admin.conf`
- [ ] 完整 `docker compose up` 需本机 Docker 引擎运行后验证

## 本地端口与环境变量

| 服务 | 端口 | 环境变量 |
|------|------|----------|
| Postgres | 5432 | `POSTGRES_USER/PASSWORD/DB=aie` |
| Control Plane HTTP | 8080 | `AIE_HTTP_ADDR`、`AIE_DATABASE_URL` |
| Control Plane gRPC | 9090 | `AIE_GRPC_ADDR` |
| Admin（compose） | 8088→80 | 反代 `/api` → control-plane |

DSN 默认：`postgres://aie:aie@localhost:5432/aie?sslmode=disable`

## 结论

协议与数据契约已冻结；Docker 实测待守护进程可用后补勾。可进入 **M2**。
