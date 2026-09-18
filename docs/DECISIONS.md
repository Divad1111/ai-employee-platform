# 架构决策记录（ADR 简表）

> 对应任务：T-0106。结论同步回填 [`DEVELOPMENT_TASKS.md`](./DEVELOPMENT_TASKS.md) 第 6 章。  
> 状态：`已决` / `暂缓` / `开放`

## M0 必须关闭（已决）

### Q-02 Feishu Employee 身份模型 — 已决

- **结论**：**B）单 Bot + 消息内解析目标 Employee**
- **理由**：飞书自建应用通常一应用一 Bot；每 Employee 一应用成本高、运维重。V1 用 @别名 / 指令前缀 / `employee_feishu_bindings` 路由到 `EMP-*`。
- **影响**：M6 Employee Resolver；绑定表存 open_id/chat 映射而非多 App Secret。
- **阻塞**：M6

### Q-03 V1 max_sessions — 已决

- **结论**：**A）V1 强制每 Employee 最多 1 个 Active Session**；配置项可保留，`>1` 时记录警告并按 1 生效。
- **理由**：与设计文档 §51 一致；§124 示例中的 `2` 留给 V3（当前暂缓）。
- **影响**：Session 服务、Workstation `MaxSessions` 默认 1。
- **阻塞**：M4

### Q-05 `aew daemon` 与 `service` — 已决

- **结论**：**A）正式增加 `aew daemon` 子命令**；`aew service install|start|...` 负责把 daemon 注册为 OS 服务并托管生命周期。
- **理由**：CLI 与长期进程入口分离，便于前台调试（直接 `daemon`）与生产托管（`service`）。
- **影响**：M5 CLI 树；文档 §40 视为已修订。
- **阻塞**：M5

---

## 其它决策（状态）

| ID | 主题 | 状态 | 备注 |
|----|------|------|------|
| Q-01 | Session 复用 vs Workspace Lock | **已决 C** | V1：每 Employee 独占一个 Workspace；不实现跨 Employee 锁竞争 |
| Q-04 | WAITING_APPROVAL / UNKNOWN 出边 | **已决** | 见下文状态图；超时停 Session 可延至 M5/M6 |
| Q-06 | Provider 签名公钥分发 | **已决 B** | Control Plane 下发公钥；见下文 |
| Q-07 | V1 Provider Install 深度 | **已决 A** | V1 仅 Detect 本机安装；完整 Registry 下载属 V2 |
| Q-08 | 无飞书时 Admin 建 Job | **已决 B** | 允许 Admin 手动 Create Job 作为 Runtime 验收并行路径 |
| Q-09–Q-11 | V3 相关 | 暂缓 | 不阻塞 V1/V2 |
| Q-12 | Event/Command 乱序 | **已决** | 严格递增、拒绝乱序、不缓冲；见 [`RELIABILITY.md`](./RELIABILITY.md) |

### Q-01 工作结论（Workspace Lock）

- **结论**：V1 每 Employee **独占**绑定一个 Workspace；创建/换绑时校验「一 Workspace 至多一个活跃 Employee」。
- Session 复用：同一 Employee 的多个 Job 可串行复用同一 Active Session（Q-03：每 Employee 最多 1 个 Active Session）。

### Q-04 Job 状态机（已决）

主路径：

```text
CREATED → QUEUED → ASSIGNED → STARTING → RUNNING → SUCCESS
```

异常与旁路：

| 状态 | 可进入自 | 可离开至 |
|------|----------|----------|
| FAILED | STARTING / RUNNING / WAITING_APPROVAL / UNKNOWN | （终态） |
| CANCELLED | CREATED…RUNNING / BLOCKED / WAITING_APPROVAL / UNKNOWN | （终态） |
| TIMEOUT | STARTING / RUNNING | （终态） |
| BLOCKED | RUNNING | RUNNING / CANCELLED / FAILED |
| WAITING_APPROVAL | RUNNING | RUNNING（批准）/ CANCELLED / FAILED |
| UNKNOWN | RUNNING（失联/Offline） | RUNNING（恢复）/ FAILED / CANCELLED |

说明：真正按 `timeout_sec` 停 Session 可在 M5/M6 完成；M4 保留字段与状态转换。


### Q-07 工作结论（便于 M5）

- **结论（暂定）**：V1 Provider **仅 Detect 本机已安装** + 路径配置；不实现完整 Registry 下载。
- 若反对，请在开发 M5 前改回本文。

### Q-08 工作结论（便于 M6）

- **结论**：**已决 B**。Golden Path **允许** Admin 手动创建 Job 验收 Runtime；飞书路径仍为实现目标但非唯一验收入口。见 [`golden_path.md`](./golden_path.md)。

### Q-06 工作结论（M9）

- **结论**：**B）Control Plane 下发公钥**。
- Workstation 通过 `GET /api/providers/signing-key` 获取 ed25519 公钥；`aew agent install --pubkey …` 校验包签名与 SHA256。
- 失败（篡改 / 错签）拒绝安装。开发态 Registry 可自生成密钥对。

---

## 变更历史

| 日期 | 说明 |
|------|------|
| 2026-09-18 | 关闭 Q-06=B；M9 Operations 落地 |
| 2026-09-18 | 关闭 Q-08=B；M6 Golden Path / TEST_MATRIX_V1 落地 |
| 2026-09-18 | 关闭 Q-02/Q-03/Q-05；记录 Q-07/Q-08 倾向；V3 决策暂缓 |
| 2026-09-18 | 关闭 Q-12 乱序策略；新增 RELIABILITY.md |
| 2026-09-18 | 关闭 Q-01/Q-04；补全 Job 状态机出边 |
