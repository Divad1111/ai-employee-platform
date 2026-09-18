# V1 Recovery / 异常矩阵（T-0507）

> 期望行为对照设计文档 §136、§53、§91。  
> **实测**：以仓库内自动化测试为准（2026-09-18）。

| 场景 | 期望行为 | 实测 | 覆盖位置 |
|------|----------|------|----------|
| Network Disconnect | Outbox 积压，恢复后续传；不丢事件 | PASS | `controlplane/outbox` |
| Server Restart | 共享 CommandStore 时 Resume 续传未 ACK | PASS | `workergrpc/connect_test` |
| Workstation Restart | last_acked Resume；RUNNING→UNKNOWN 不标 SUCCESS | PASS | `workergrpc` + `runtime/recovery` + `resume.TestBuildHelloFrame` |
| Cursor Crash | Session/Job → UNKNOWN，不标 SUCCESS | PASS | `runtime/recovery` |
| ACP Disconnect | MarkUnknown | PASS | `runtime/recovery` |
| Duplicate Command | command_id 幂等 | PASS | `controlplane/sequence` + `ack` |
| Certificate Revoked | mTLS 拒绝 | PASS | `workergrpc/mtls_test` |
| Incomplete Identity Dial | 缺证书拒绝 Dial | PASS | `grpc.TestDialRejectsIncompleteIdentity` |
| Job Timeout | RUNNING→TIMEOUT 终态不可恢复 | PASS | `job.TestJobTimeoutFromRunning` |
| Workspace Missing | Scheduler 拒绝分配 | PASS | `scheduler.TestWorkspaceMissingRejects` |
| Provider Missing | Detect 空路径；Fake ACP 可开发态启动 | PASS | `providers/cursor` |
| Offline WS | 不分新 Job；RUNNING→UNKNOWN | PASS | `scheduler` + `presence.OnOffline` |
| Duplicate Feishu event_id | 幂等 200，不双建 Job | PASS | `feishu` + `api/feishu_api_test` |
| Feishu 验签失败 | 401 | PASS | `api/feishu_api_test` |
| Secret 明文 | 不进配置回显/普通审计 | PASS | `secret` + `api` |
| Identity 本地私钥 | Generate/Save/Load；私钥不上传 | PASS | `identity.TestGenerateSaveLoadClear` |

## Golden Path 清单勾选（第 4 章）

见开发任务文档第 4 章；核心链路已由 M0–M6 自动化测试覆盖。飞书真实租户联调需配置 Secret 后人工点验。
