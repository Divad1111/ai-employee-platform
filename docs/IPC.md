# Workstation IPC 权限说明（M5）

> 对应任务：T-0602。

## 传输

| 平台 | 实现 | 权限约束 |
|------|------|----------|
| Windows | `127.0.0.1` TCP + `%ProgramData%/AIEmployee/data/aew.ipc` 端口文件 | 仅 loopback；端口文件 `0600`，目录 ACL 限制本机用户 |
| Unix | UDS（`/tmp/aie-aew-$USER.sock`） | socket 文件模式 `0600`，仅属主可连 |
| 测试 | `MemoryTransport` | 进程内 |

CLI 通过 IPC 调用 Daemon；**不直接**读写 SQLite 做核心状态变更。

后续可将 Windows 升级为 Named Pipe + 显式 SDDL（Administrators / SYSTEM / Owner）。
