# AI Employee System — Token Usage 统一采集与任务统计设计

> 文档用途：本文件用于直接指导 Codex 在现有 AI Employee System 中实现“任务真实 Token 消耗统计”功能。
>
> 核心目标：Codex、Cursor 等 Agent Runtime 分别实现 Token Usage Provider，Workstation 通过统一接口获取真实 Token 消耗，并在任务执行过程中采集、任务完成后汇总并写入 Center Server 的 Task。

---

# 1. 背景与目标

当前系统架构：

```text
Center Server
    │
    ├── Admin Web
    │
    └── Workstation
            │
            └── Digital Employee
                    │
                    └── Agent Runtime
                         ├── Codex
                         ├── Cursor
                         └── Other Agent
```

系统需要统计每个 Task 的真实 Token 消耗。

要求：

1. 不能通过文本长度、tiktoken 等方式估算。
2. 必须优先使用 Agent Runtime 自身提供的真实 usage。
3. Codex、Cursor 分别实现自己的 Provider。
4. Workstation 对上层提供统一的 Token Usage 接口。
5. Center Server 不需要知道 Codex、Cursor 的具体实现。
6. 一个 Task 可以包含多个 Agent Run / Turn，必须支持累加。
7. 任务执行期间实时采集。
8. Task 完成时生成最终汇总。
9. 如果 usage 暂时不可用，支持后台重试。
10. 后续可以扩展 Claude Code、Gemini CLI、OpenCode 等，不修改 Center Server 的核心任务模型。
11. Token Usage 与 Billing / Cost 分离，本阶段只实现 Token Usage。
12. 所有 Token Usage 必须能够追溯到 Task、Digital Employee、Workstation、Provider、Session、Run。

---

# 2. 核心设计原则

## 2.1 不允许 Workstation 自行估算 Token

禁止：

```text
用户输入文本
    ↓
Tokenizer
    ↓
估算 Token
```

这种数据不能作为真实 Token Usage。

正确方式：

```text
Codex / Cursor
    ↓
Agent Runtime 官方 usage
    ↓
Provider Adapter
    ↓
统一 TokenUsage
    ↓
Workstation
    ↓
Center Server
```

---

# 3. 统一架构

```text
                         Center Server
                              │
                         Task Service
                              │
                 Generic Token Usage API
                              │
                              ↓
                         Workstation
                              │
                     Token Usage Service
                              │
                 ┌────────────┴────────────┐
                 │                         │
                 ↓                         ↓
        CodexUsageProvider        CursorUsageProvider
                 │                         │
                 ↓                         ↓
             Codex                   Cursor SDK/API
                 │                         │
                 └────────────┬────────────┘
                              ↓
                        TokenUsage
                              │
                              ↓
                       Usage Collector
                              │
                              ↓
                         Task Usage
```

Center Server 不允许出现：

```go
if provider == "codex" {
}

if provider == "cursor" {
}
```

Provider 差异全部封装在 Workstation。

---

# 4. Token Usage 数据模型

定义统一模型：

```go
type TokenUsage struct {
    InputTokens           int64 `json:"input_tokens"`
    CachedInputTokens     int64 `json:"cached_input_tokens"`
    CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
    CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`

    OutputTokens          int64 `json:"output_tokens"`
    ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`

    TotalTokens           int64 `json:"total_tokens"`

    Provider              string `json:"provider"`
    ProviderSessionID     string `json:"provider_session_id,omitempty"`
    ProviderRunID         string `json:"provider_run_id,omitempty"`

    Source                string `json:"source"`

    UsageStatus           string `json:"usage_status"`
}
```

`usage_status`：

```text
PENDING
PARTIAL
FINAL
UNAVAILABLE
```

`source` 示例：

```text
codex_cli_event
codex_sdk
cursor_sdk
cursor_api
```

---

# 5. 字段语义

## 5.1 InputTokens

模型实际接收的 input tokens。

不是简单的用户消息长度。

---

## 5.2 CachedInputTokens

Provider 返回的 cached input token 数量。

必须原样记录。

---

## 5.3 CacheWriteInputTokens

Provider 支持时记录 cache write input tokens。

Provider 不支持时：

```text
0
```

---

## 5.4 CacheReadInputTokens

Provider 支持时记录 cache read input tokens。

Provider 不支持时：

```text
0
```

---

## 5.5 OutputTokens

模型输出 token。

---

## 5.6 ReasoningOutputTokens

Provider 提供 reasoning token 时记录。

Provider 不支持时：

```text
0
```

---

## 5.7 TotalTokens

优先使用 Provider 自己提供的 total。

如果 Provider 没有 total，可以按照明确的 Provider 语义计算，但不要自行重新解释 Provider 字段。

默认：

```text
TotalTokens =
    InputTokens
    + OutputTokens
```

注意：

`CachedInputTokens` 不应再次加到 TotalTokens，避免重复统计。

---

# 6. Provider 接口

Workstation 定义统一接口：

```go
type TokenUsageProvider interface {
    Name() string

    Supports(runtime AgentRuntime) bool

    GetRunUsage(
        ctx context.Context,
        execution *AgentExecution,
    ) (*TokenUsage, error)
}
```

建议：

```go
type AgentRuntime string

const (
    AgentRuntimeCodex   AgentRuntime = "codex"
    AgentRuntimeCursor  AgentRuntime = "cursor"
)
```

Provider 注册：

```go
type TokenUsageRegistry struct {
    providers map[string]TokenUsageProvider
}
```

启动时：

```go
registry.Register(NewCodexUsageProvider(...))
registry.Register(NewCursorUsageProvider(...))
```

未来：

```go
registry.Register(NewClaudeCodeUsageProvider(...))
registry.Register(NewGeminiUsageProvider(...))
registry.Register(NewOpenCodeUsageProvider(...))
```

不修改 Task Service。

---

# 7. AgentExecution

Token Usage 必须绑定到具体 Agent Execution。

建议：

```go
type AgentExecution struct {
    TaskID             string
    DigitalEmployeeID  string
    WorkstationID      string

    Runtime            AgentRuntime

    ProviderSessionID  string
    ProviderRunID      string

    StartedAt          time.Time
    FinishedAt         *time.Time
}
```

关系：

```text
Task
 │
 ├── Digital Employee
 │
 ├── Workstation
 │
 └── AgentExecution
       │
       ├── Provider
       ├── Session
       └── Run
```

---

# 8. Codex Provider

## 8.1 原则

Codex Provider 只负责：

```text
Codex
 ↓
获取真实 usage
 ↓
转换为 TokenUsage
```

不得将 Codex 特有字段泄漏到公共 Task Service。

---

## 8.2 Codex JSON Event

如果当前 Workstation 使用：

```bash
codex exec --json
```

应监听 JSONL event。

重点处理：

```text
turn.completed
```

其 usage 可能包含：

```json
{
  "input_tokens": 24763,
  "cached_input_tokens": 24448,
  "cache_write_input_tokens": 1000,
  "output_tokens": 122,
  "reasoning_output_tokens": 80
}
```

具体字段以当前实际安装的 Codex CLI / SDK 返回为准。

实现时不要假设未来版本字段永远不变。

---

## 8.3 Codex Provider 示例

```go
type CodexUsageProvider struct {
}

func (p *CodexUsageProvider) Name() string {
    return "codex"
}

func (p *CodexUsageProvider) Supports(
    runtime AgentRuntime,
) bool {
    return runtime == AgentRuntimeCodex
}

func (p *CodexUsageProvider) GetRunUsage(
    ctx context.Context,
    execution *AgentExecution,
) (*TokenUsage, error) {
    // 从当前 Codex execution/session 中获取最终 usage
}
```

---

# 9. Cursor Provider

Cursor Provider 同样只负责 Adapter。

如果当前 Cursor SDK 支持通过 Run ID 查询：

```text
agent.getUsage()
```

则使用：

```text
ProviderRunID
    ↓
Cursor Usage API / SDK
    ↓
TokenUsage
```

Cursor usage 可能包含：

```text
inputTokens
outputTokens
cacheWriteTokens
cacheReadTokens
totalTokens
```

具体字段以当前实际 Cursor SDK/API 为准。

---

# 10. Usage Collector

Workstation 增加：

```go
type UsageCollector interface {
    Start(execution *AgentExecution) error

    Record(
        execution *AgentExecution,
        usage *TokenUsage,
    ) error

    Finalize(
        taskID string,
    ) (*TokenUsageSummary, error)
}
```

作用：

```text
Provider
   ↓
UsageCollector
   ↓
保存 Run Usage
   ↓
Task 完成
   ↓
汇总
```

---

# 11. 为什么不能只保存 Task.token_usage

错误设计：

```text
Task
 └── token_usage
```

原因：

一个 Task 可能执行多次 Agent Run。

例如：

```text
Task 1001
 ├── Codex Run 1
 ├── Codex Run 2
 └── Codex Run 3
```

所以必须保存明细。

---

# 12. Task Token Usage 明细表

建议 Center Server 增加：

```text
task_token_usage
```

字段：

```text
id
task_id

workstation_id
digital_employee_id

provider
provider_session_id
provider_run_id

input_tokens
cached_input_tokens
cache_write_input_tokens
cache_read_input_tokens

output_tokens
reasoning_output_tokens

total_tokens

usage_status
usage_source

created_at
updated_at
```

---

# 13. 唯一性与幂等

必须避免重复写入。

推荐唯一键：

```text
(provider, provider_run_id)
```

如果 Provider Run ID 不可靠，则：

```text
(task_id, provider_session_id, provider_run_id)
```

API 必须支持幂等。

例如：

```http
PUT /api/tasks/{task_id}/token-usage/{usage_id}
```

或者：

```http
POST /api/tasks/{task_id}/token-usage
Idempotency-Key: ...
```

重复上传同一个 Run Usage 不得产生重复统计。

---

# 14. Task 汇总模型

定义：

```go
type TokenUsageSummary struct {
    InputTokens           int64 `json:"input_tokens"`
    CachedInputTokens     int64 `json:"cached_input_tokens"`
    CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
    CacheReadInputTokens  int64 `json:"cache_read_input_tokens"`

    OutputTokens          int64 `json:"output_tokens"`
    ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`

    TotalTokens           int64 `json:"total_tokens"`

    UsageStatus           string `json:"usage_status"`
}
```

汇总规则：

```text
Task Input
    = SUM(all runs.input_tokens)

Task Output
    = SUM(all runs.output_tokens)

Task Reasoning
    = SUM(all runs.reasoning_output_tokens)

Task Total
    = SUM(all runs.total_tokens)
```

---

# 15. 任务生命周期

推荐：

```text
TASK CREATED
      │
      ↓
TASK QUEUED
      │
      ↓
TASK RUNNING
      │
      ↓
AgentExecution START
      │
      ↓
Agent Runtime
      │
      ├── Usage Event
      ├── Usage Event
      └── Usage Event
      │
      ↓
AgentExecution FINISHED
      │
      ↓
GetRunUsage()
      │
      ↓
Record()
      │
      ↓
Task Finished
      │
      ↓
Finalize()
      │
      ↓
Upload to Center
```

---

# 16. 推荐：实时采集 + 最终确认

不要只在 Task 完成后查询。

正确：

```text
Agent 执行过程中
    ↓
实时收到 usage
    ↓
本地保存
```

Task 完成：

```text
Task Completed
    ↓
再次获取最终 Run Usage
    ↓
覆盖/更新对应 Run
    ↓
重新汇总
    ↓
FINAL
```

这样可以防止：

- 进程崩溃
- 网络中断
- Agent 输出异常
- 最后一个 event 延迟
- Provider API 延迟

导致 usage 丢失。

---

# 17. Usage Status

## PENDING

任务已经完成，但 Provider 尚未返回最终 usage。

---

## PARTIAL

已经获得部分 usage。

例如：

```text
Run 1 FINAL
Run 2 FINAL
Run 3 PENDING
```

Task：

```text
PARTIAL
```

---

## FINAL

所有可确认的 Run 都已经获得最终 usage。

---

## UNAVAILABLE

Provider 明确无法获取 usage。

此时不能用估算值冒充真实值。

---

# 18. Workstation 本地持久化

强烈建议 Workstation 本地也保存 Token Usage。

原因：

```text
Agent
 ↓
Workstation
 ↓
Center
```

如果：

```text
Workstation → Center
```

网络断开：

```text
Token Usage
```

不能丢。

因此：

```text
Workstation SQLite
    │
    ├── task
    ├── agent_execution
    └── token_usage
```

Center 恢复后：

```text
Outbox
 ↓
Retry
 ↓
Center
```

---

# 19. Workstation Outbox

推荐使用已有任务事件 / Outbox 机制。

Token Usage 事件：

```json
{
  "event_type": "TASK_TOKEN_USAGE_UPDATED",
  "task_id": "task_xxx",
  "execution_id": "execution_xxx",
  "usage": {
    "provider": "codex",
    "provider_run_id": "run_xxx",
    "input_tokens": 10000,
    "output_tokens": 500,
    "total_tokens": 10500,
    "usage_status": "FINAL"
  }
}
```

事件必须幂等。

---

# 20. Center API

建议增加：

```http
POST /api/tasks/{task_id}/token-usage
```

请求：

```json
{
  "execution_id": "execution_xxx",

  "provider": "codex",
  "provider_session_id": "session_xxx",
  "provider_run_id": "run_xxx",

  "input_tokens": 10000,
  "cached_input_tokens": 8000,
  "cache_write_input_tokens": 0,
  "cache_read_input_tokens": 0,

  "output_tokens": 500,
  "reasoning_output_tokens": 100,

  "total_tokens": 10500,

  "usage_status": "FINAL",
  "usage_source": "codex_cli_event"
}
```

返回：

```json
{
  "success": true,
  "usage_id": "usage_xxx"
}
```

---

# 21. 查询 API

任务详情：

```http
GET /api/tasks/{task_id}/token-usage
```

返回：

```json
{
  "task_id": "task_xxx",

  "summary": {
    "input_tokens": 30500,
    "cached_input_tokens": 23000,
    "output_tokens": 3500,
    "reasoning_output_tokens": 1200,
    "total_tokens": 34400,
    "usage_status": "FINAL"
  },

  "runs": [
    {
      "provider": "codex",
      "provider_run_id": "run_1",
      "input_tokens": 12300,
      "output_tokens": 1200,
      "total_tokens": 13500,
      "usage_status": "FINAL"
    },
    {
      "provider": "codex",
      "provider_run_id": "run_2",
      "input_tokens": 18200,
      "output_tokens": 2300,
      "total_tokens": 20900,
      "usage_status": "FINAL"
    }
  ]
}
```

---

# 22. Task 表是否直接增加 Token 字段

推荐两种方式：

## 明细

`task_token_usage`

保存所有 Run。

## 汇总

Task 表可以增加：

```text
token_input_tokens
token_output_tokens
token_total_tokens
token_usage_status
```

这些字段只是缓存/汇总，不是权威明细。

权威数据：

```text
task_token_usage
```

这样 Admin Web 查询 Task 列表时无需每次 SUM 明细。

---

# 23. Admin Web

任务详情增加：

```text
Token Usage
```

显示：

```text
Input Tokens          30,500
Cached Input          23,000
Output Tokens          3,500
Reasoning              1,200
Total Tokens          34,400

Status                 FINAL
```

下面显示 Run 明细：

```text
Provider    Run ID      Input    Output    Total
--------------------------------------------------
Codex       run_001     12,300   1,200     13,500
Codex       run_002     18,200   2,300     20,500
```

---

# 24. Token Usage 统计

未来 Admin Web 可以增加：

```text
Token Statistics
```

维度：

```text
按时间
按 Workstation
按 User
按 Digital Employee
按 Provider
按 Task
```

例如：

```text
Digital Employee: Unity程序员

Tasks                 126
Input Tokens       1.82M
Output Tokens      320K
Reasoning Tokens   110K
Total Tokens       2.25M
```

注意：

统计系统只读取统一 TokenUsage，不关心 Provider 实现。

---

# 25. Provider Factory

推荐：

```go
func NewTokenUsageProvider(
    runtime AgentRuntime,
) TokenUsageProvider
```

或者 Registry：

```go
type TokenUsageRegistry struct {
    providers map[string]TokenUsageProvider
}

func (r *TokenUsageRegistry) Get(
    runtime AgentRuntime,
) (TokenUsageProvider, error)
```

不要在 Task Service 里面写 Provider 判断。

---

# 26. 错误处理

Provider 获取失败必须区分：

```text
ProviderNotSupported
ProviderRunNotFound
UsageNotAvailable
ProviderTemporaryError
ProviderAuthenticationError
ProviderParseError
```

例如：

```go
var (
    ErrProviderNotSupported = errors.New(...)
    ErrUsageNotAvailable    = errors.New(...)
    ErrProviderTemporary    = errors.New(...)
)
```

---

# 27. 重试策略

对于：

```text
ProviderTemporaryError
UsageNotAvailable
```

采用指数退避。

例如：

```text
5s
15s
30s
60s
5min
15min
30min
```

最大重试时间建议：

```text
24 hours
```

超过后：

```text
UNAVAILABLE
```

但不要伪造 Token。

---

# 28. Provider 版本兼容

Codex / Cursor CLI / SDK 都可能升级。

Provider 实现必须：

1. 对未知字段容忍。
2. 不因为新增字段导致解析失败。
3. 缺少可选字段时使用 0。
4. 保存 `usage_source`。
5. 尽可能保存 Provider 原始 Run ID。
6. 不依赖 UI 文本解析作为第一方案。

推荐：

```text
CLI JSON / SDK / API
        ↓
Structured Parser
        ↓
TokenUsage
```

禁止优先解析：

```text
终端显示文本
```

---

# 29. 不允许的实现

## 29.1 不允许通过文本估算

错误：

```go
tokens := len(text) / 4
```

---

## 29.2 不允许 Center 解析 Codex 日志

错误：

```text
Center
 ↓
Codex Log
 ↓
解析
```

---

## 29.3 不允许 Cursor/Codex 直接修改 Center Task 内部结构

Provider 只输出统一 TokenUsage。

---

## 29.4 不允许重复计算 Cache Token

例如：

```text
InputTokens = 10000
CachedInputTokens = 8000
```

不能：

```text
Total = 10000 + 8000
```

除非 Provider 明确将两个字段定义为非重叠 token。

---

# 30. 安全要求

Token Usage API 必须继承现有 Workstation ↔ Center 的认证机制。

必须校验：

```text
workstation_id
task_id
execution_id
digital_employee_id
```

不能允许一个 Workstation 上传其他 Workstation 的 Task Usage。

Provider Run ID 不能单独作为权限依据。

---

# 31. 数据一致性

必须保证：

```text
Run Usage
    ↓
Task Summary
```

可重新计算。

也就是说：

```sql
SUM(task_token_usage.total_tokens)
```

应该可以得到 Task Total。

Task 表里的汇总字段只是缓存。

如果缓存损坏，可以重新计算。

---

# 32. 任务重试场景

例如：

```text
Task 1001
 ↓
Codex Run 1
 ↓
失败
 ↓
Task Retry
 ↓
Codex Run 2
 ↓
成功
```

不能覆盖 Run 1。

应该：

```text
Task 1001
 ├── Run 1
 │    └── Usage
 │
 └── Run 2
      └── Usage
```

Task 总 Token：

```text
Run1 + Run2
```

如果产品未来需要区分：

```text
有效消耗
失败消耗
重试消耗
```

可以增加：

```text
execution_status
```

但本阶段不需要改变 TokenUsage 核心结构。

---

# 33. 一个 Task 多 Agent Runtime

未来可能：

```text
Task
 ├── Cursor Run 1
 ├── Codex Run 2
 └── Codex Run 3
```

统一处理：

```text
task_token_usage
```

记录：

```text
provider = cursor
provider = codex
provider = codex
```

最终：

```text
Task Total
 = Cursor
 + Codex
 + Codex
```

Center 无需特殊处理。

---

# 34. 与 Digital Employee 的关系

每个 Token Usage 必须能追溯：

```text
User
 ↓
Digital Employee
 ↓
Workstation
 ↓
Task
 ↓
AgentExecution
 ↓
Provider
 ↓
Provider Run
 ↓
Token Usage
```

这样未来可以统计：

```text
某员工的 AI 消耗
某数字员工的 Token 消耗
某工作站的 Token 消耗
某 Agent 的 Token 消耗
某任务的 Token 消耗
```

---

# 35. 与用户权限的关系

Token Usage 不应该成为新的权限模型。

读取权限：

```text
Task View Permission
```

即可控制 Task Token Usage 查看权限。

如果未来需要：

```text
Token Usage Admin
```

再单独增加统计权限。

本阶段：

```text
能查看 Task
    ↓
可以查看该 Task Token Usage
```

---

# 36. 实现目录建议

Workstation：

```text
internal/
  agent/
    runtime/
      codex/
      cursor/

  tokenusage/
    provider.go
    registry.go
    collector.go
    aggregator.go
    model.go
    errors.go

    codex/
      provider.go
      parser.go

    cursor/
      provider.go
      parser.go

  task/
    executor.go
```

Center：

```text
internal/
  task/
    service.go
    repository.go

  tokenusage/
    model.go
    service.go
    repository.go

  api/
    task.go
    token_usage.go
```

Admin Web：

```text
src/
  pages/
    tasks/
      TaskDetail/
        TokenUsagePanel
        TokenUsageRunsTable
```

实际目录应优先适配现有项目结构，不要为了匹配本文档强行重构现有代码。

---

# 37. 推荐实现顺序

## Phase 1 — Workstation Domain

实现：

```text
TokenUsage
TokenUsageSummary
AgentExecution
TokenUsageProvider
TokenUsageRegistry
UsageCollector
```

---

## Phase 2 — Codex

实现：

```text
CodexUsageProvider
Codex JSON Event Parser
turn.completed usage
```

确保真实 usage 可以获取。

---

## Phase 3 — Cursor

实现：

```text
CursorUsageProvider
Cursor Run Usage Query
```

---

## Phase 4 — Workstation Persistence

增加 SQLite：

```text
agent_execution
token_usage
```

以及 Outbox。

---

## Phase 5 — Center API

实现：

```text
POST /api/tasks/{task_id}/token-usage
GET  /api/tasks/{task_id}/token-usage
```

实现幂等。

---

## Phase 6 — Task Integration

Task 完成：

```text
Finalize Usage
 ↓
Aggregate
 ↓
Upload
```

---

## Phase 7 — Admin Web

Task Detail：

```text
Token Usage
```

以及 Run 明细。

---

## Phase 8 — Statistics

实现：

```text
按 User
按 Digital Employee
按 Workstation
按 Provider
按时间
```

---

# 38. 单元测试

必须测试：

## Codex

```text
正常 usage
缺少可选字段
未知字段
多个 turn
重复 turn
```

---

## Cursor

```text
正常 usage
run 不存在
usage 延迟
API 临时失败
```

---

## Aggregator

```text
Run1 + Run2
多 Provider
重复上传
0 token
PARTIAL → FINAL
```

---

## Task Retry

```text
Run1 failed
Run2 success
```

确保两个 Run 都保留。

---

# 39. 集成测试

测试：

```text
Feishu Task
 ↓
Center
 ↓
Workstation
 ↓
Digital Employee
 ↓
Codex
 ↓
真实 usage
 ↓
Workstation SQLite
 ↓
Center
 ↓
Task
```

以及：

```text
Cursor
 ↓
真实 usage
 ↓
Center
```

---

# 40. 断网测试

测试：

```text
Agent
 ↓
usage
 ↓
Workstation
 ↓
Center 网络断开
```

要求：

```text
usage 不丢
```

网络恢复：

```text
Outbox
 ↓
Retry
 ↓
Center
```

最终：

```text
FINAL
```

---

# 41. 崩溃恢复测试

场景：

```text
Agent 执行
 ↓
已经获得 usage
 ↓
Workstation crash
```

重启后：

```text
读取本地 execution
 ↓
重新查询 Provider usage
 ↓
幂等更新
```

不能重复累计。

---

# 42. 验收标准

功能完成必须满足：

- [ ] Workstation 有统一 `TokenUsageProvider`
- [ ] Codex 独立 Provider
- [ ] Cursor 独立 Provider
- [ ] Center 不包含 Codex/Cursor 特殊逻辑
- [ ] 不允许 Token 估算冒充真实 usage
- [ ] 支持一个 Task 多 Run
- [ ] 支持一个 Task 多 Provider
- [ ] Workstation 本地持久化 usage
- [ ] Center 支持幂等上传
- [ ] 支持断网重试
- [ ] 支持 usage PENDING/PARTIAL/FINAL/UNAVAILABLE
- [ ] Task 完成后可以得到最终汇总
- [ ] Task Detail 可以查看 Token Usage
- [ ] 可以查看每个 Run 的 Token Usage
- [ ] Task Total 可以由明细重新计算
- [ ] 重试不会覆盖历史 Run
- [ ] Workstation crash 后不会产生重复 Token
- [ ] 未知 Provider 字段不会导致解析失败
- [ ] 后续增加 Provider 不需要修改 Center Task 核心逻辑

---

# 43. Codex 开发要求

Codex 开发时遵循以下原则：

1. **先检查现有项目结构和 Task 生命周期。**
2. 不要重复创建已有的 Task、Event、Outbox、Repository 抽象。
3. 优先复用现有 Workstation ↔ Center 通信机制。
4. 优先复用现有认证、签名、幂等和重试机制。
5. Token Usage 是一个独立能力模块。
6. Provider 只负责把 Agent Runtime 的真实 usage 转换成统一模型。
7. 不允许 Center Server 解析 Agent Runtime 的日志。
8. 不允许通过文本长度估算真实 Token。
9. 不允许因为某 Provider 不支持某字段而阻塞整个 Task。
10. Provider 不支持的可选字段使用 0。
11. 真实 usage 无法获得时必须明确标记 `UNAVAILABLE`，不能伪造数据。
12. 所有数据库迁移必须可重复执行并兼容已有数据。
13. 所有 API 必须加入现有认证和权限体系。
14. 所有新增逻辑必须有单元测试。
15. 关键生命周期必须有集成测试。
16. 不要为了本功能大规模重构现有系统。

---

# 44. 最终目标架构

最终形成：

```text
                         ┌──────────────────────┐
                         │    Center Server     │
                         │                      │
                         │ Task Service         │
                         │ Token Usage Service  │
                         │ Admin Web            │
                         └──────────┬───────────┘
                                    │
                         Generic Task / Usage API
                                    │
                                    ↓
                         ┌──────────────────────┐
                         │     Workstation      │
                         │                      │
                         │ Task Executor        │
                         │ Usage Collector      │
                         │ Usage Aggregator     │
                         │ SQLite / Outbox      │
                         └──────────┬───────────┘
                                    │
                         Agent Runtime
                                    │
                    ┌───────────────┼───────────────┐
                    │               │               │
                    ↓               ↓               ↓
                 Codex           Cursor          Future
                    │               │               │
                    ↓               ↓               ↓
             Codex Provider   Cursor Provider   Xxx Provider
                    │               │               │
                    └───────────────┼───────────────┘
                                    ↓
                              TokenUsage
                                    │
                                    ↓
                              Task Usage
```

核心原则：

> **Provider 负责知道“怎么从具体 Agent 拿到真实 Token”；Workstation 负责统一采集、持久化、汇总和上报；Center 只负责保存、查询、统计和展示统一 Token Usage。**

这样以后无论增加多少种 Agent Runtime，都不会破坏现有任务系统。
