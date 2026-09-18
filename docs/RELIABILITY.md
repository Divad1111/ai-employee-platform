# 可靠通信约定（M3）

> 对应任务：T-0803 乱序策略文档化。

## 序号与幂等

| 方向 | 序号 | 重复键 | 行为 |
|------|------|--------|------|
| Server→Worker Command | 每 Workstation 单调递增 | `command_id` / `message_id` | 重复 → 幂等成功（不重复执行）；`sequence <= last` → REJECT |
| Worker→Server Event | 每 Workstation 单调递增 | `event_id` / `message_id` | 同上 |

## 乱序策略（已决）

**严格递增，拒绝乱序，不缓冲。**

- 期望 `sequence == last_sequence + 1`
- 跳号 / 乱序 → REJECT（`accepted=false` + error_message）
- 理由：V1 单流有序投递；缓冲会增加状态复杂度与重复执行风险

## ACK

ACK 表示「本地 SQLite（或等价 Journal）已 COMMIT」，不是「收到」。COMMIT 失败不发送 ACK；崩溃恢复后未 ACK 命令可由 Server Resume 重投，靠 `command_id` 幂等。

## Resume

Connect 首帧 `WorkerHello.last_acked_command_sequence = N` → Server 从 `N+1` 续传未确认 Command。

## Heartbeat / Offline

- 默认心跳间隔约 **5s**，约 **15s** 无心跳 → `OFFLINE`（可配置）
- Offline 后 Scheduler（M6）不得分配新 Job；已有 RUNNING → `UNKNOWN`（不立即 FAILED）

## Reconnect

状态：`CONNECTED → DISCONNECTED → RECONNECTING → CONNECTED`；退避 1s/2s/4s/…，默认上限 60s。
