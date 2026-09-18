# V1 Golden Path（半自动验收）

> 对应任务：T-0506。对齐设计文档 §131、§132 与 `DEVELOPMENT_TASKS.md` 第 4 章。
> Q-08：**允许** Admin 手动 Create Job 作为飞书路径的并行验收。

## 前置

```bash
# Control Plane
cd server && go run ./cmd/server

# Admin（另开终端）
cd admin && npm run dev

# Workstation
cd workstation && go run ./cmd/aew daemon --skip-connect
# 或完整：先 register 再去掉 --skip-connect
```

## 路径 A：Admin 手动 Job（无飞书）

1. 登录 Admin（`admin` / `admin123`）
2. 创建 Employee，绑定 `workstation_id` / `workspace_id`
3. Workstation Online（daemon Connect 或测试 Touch）
4. Admin → Jobs → 创建 Job → Timeline 可见
5.（可选）`POST /api/scheduler/tick` 下发 START_JOB

## 路径 B：飞书 @Employee

1. `PUT /api/integrations/feishu/config` 配置 App ID；`app_secret` 仅存 Secret 引用
2. `POST /api/integrations/feishu/bindings`：`feishu_bot_alias=dev` → `employee_id`
3. 飞书后台 Webhook → `POST /api/integrations/feishu/events`
4. 消息文本：`@dev 修复空指针` 或 `/emp EMP-xxx ...`
5. 立即 200；Job 入队；终态后飞书会话收到回复

## 自动化覆盖

```bash
cd server && go test ./internal/feishu/... ./internal/scheduler/... ./internal/secret/... ./internal/api/... -count=1
cd workstation && go test ./... -count=1
```

详见 [`TEST_MATRIX_V1.md`](./TEST_MATRIX_V1.md)。
