# 飞书员工助手命令

## 使用

命令可以直接发给机器人。群聊若需要 @机器人来触发消息订阅, 可以写 `@机器人 /help`。不符合派单格式的私聊、未知命令、发给机器人的不支持格式会显示派单示例和命令说明。群聊普通闲聊不回复。

- `/help`: 查看帮助, 别名 `/h`、`/?`。
- `/list emp`: 查看已登记飞书别名的员工及可派单状态, 包含暂不可派单的员工, 别名 `/employees`、`/list employees`。
- `/model [员工]`: 查看员工关联工作站最近上报的模型; 省略员工时按已登记员工分组, 别名 `/models`。离线时可能显示缓存目录。
- `/status 员工`: 查看员工状态、工作站状态、默认 Provider 和默认模型。默认配置用于后续任务, 已运行任务以任务卡片为准。
- `/whoami`: 查看当前飞书身份已关联的员工。
- `/files`: 查看自己在当前会话的待派单附件, 以及最近任务已使用的附件。派单后仍能查到任务号和文件名; 历史不自动用于新任务。
- `/clear files`: 清空自己在当前会话暂存的附件。
- `/change model 员工 [Provider] 模型`: 修改员工后续任务使用的模型。`/change mode` 为兼容别名。省略 Provider 时使用员工当前默认 Provider。模型必须是工作站上报的 ID 或完整显示名称, 名称有空格时无需引号。
- `/new 员工`: 新建员工会话, 下一条任务使用新上下文。不会取消正在运行的任务。别名 `/new-session`、`/new_session`。

员工参数支持别名、`@别名`、`EMP-xxxx`。示例:

```text
/help
/list emp
/model test3
/status @test3
/whoami
/change model test3 codex 工作站上报的模型ID
/change mode test3 工作站上报的完整模型名称
/new test3
```

也支持员工前置的写法:

```text
@test3 /status
@test3 /model
@test3 /change model codex 工作站上报的模型ID
@test3 /new
/emp test3 /status
```

已有的 `@员工 /model 模型`、`@员工 切换模型 模型`、`@员工 新建会话` 继续可用。`@员工 /model` 不带参数改为查看模型目录。模型切换、新建会话都要求发送者事先绑定目标员工身份, 控制命令不会自动建立身份绑定。

派单格式继续支持 `@员工 任务需求`、`/emp 别名 任务需求`、`EMP-xxxx 任务需求`。入站支持文本、富文本、图片和文件。可以先发送附件再点名员工派单, 或回复附件消息并派单; 同条富文本中的图片和文件可以随任务一起发送。详见 [图片和文件使用说明](FEISHU_ATTACHMENTS.md)。语音等尚未支持的类型仍会提示用法。未知斜杠命令不会作为 AI 任务派发。

## 代码组织与扩展约定

- `server/internal/feishu/commands.go`: 命令描述、别名、帮助生成、匹配、参数校验和回复。只匹配完整命令词, `/helpful` 不会匹配 `/help`。新增命令在 `chatCommandSpecs()` 注册一个描述和处理器, 不需要修改任务分发流程。
- `CommandDirectory` 和 `command_bridge.go`: 员工信息、工作站状态和模型目录查询接口及适配。结果来自已有服务, 不维护另一份员工或模型配置。
- `taskconversation.Service`: 复用已有模型校验、员工配置更新、新建会话与审计逻辑。修改操作校验已有飞书身份绑定, 不依赖工作站当前可派单状态。
- `service.go`: 事件去重、格式判断、决策回复、命令分发、任务派发。命令处理在自动绑定和任务创建之前。
- `card.go`: 原有派单引导与命令帮助共用注册表生成的命令说明。

查询命令不创建 Job。控制命令只修改员工配置/会话代数。暂未接入 `/task`、`/cancel`、`/retry`; 后续可用同一注册表扩展, 分别接入任务查询、原有取消服务和重试服务, 同时验证任务归属、状态约束与幂等性。不要仅在帮助中展示尚未实现的命令。

## MCP 认证方式

后台身份凭证列表的认证方式展示来自该凭证的 `auth_type`, `BASIC` 代表这条凭证配置了 Basic Auth。员工派发 MCP 时通过绑定的 `credential_id` 读取该凭证的类型、密钥和注入配置, 并非统一写死 Basic。

修改关联凭证后, 下一次任务派发即按新配置生成请求头或环境变量。不会仅根据 MCP 服务名称猜测认证方式。已运行会话中的认证不会主动替换。详细配置见 [WORKFLOW_MCP.md](WORKFLOW_MCP.md#外部-mcp-身份凭证)。

## 验证

```powershell
go test ./server/internal/feishu ./server/internal/taskconversation ./server/internal/mcp ./server/internal/scheduler ./server/internal/api ./server/cmd/server
```

自动测试覆盖命令入口、中文/英文模型名称、身份绑定隔离、未知命令、普通派单回归、不支持消息格式及凭证类型变更后派发认证同步变化。真实飞书收发需部署后在私聊和群聊各做一次验收。
