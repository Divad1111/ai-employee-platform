# 工作流MCP

Control Plane 内嵌的「工作流 / 技能包 / 知识库」管理子系统，参考 `PersonalWorkMCP`（只读）用 Go 重写，并以「**工作流单点授权**」权限模型整合进 AI 员工平台。

## 权限模型

1. **Admin RBAC**：`workflow.read` / `workflow.write` / `workflow.delete` / `workflow.grant`
2. **员工授权**：仅 `employee_workflows` 表；技能与知识通过工作流的 `skills` / `knowledge` 引用闭包自动放行（`knowledge` 支持 `*` 通配）
3. **MCP 双鉴权**
   - `aiemcp_` 前缀 Token：`EMPLOYEE/READ`（Scope 过滤只读）或 `USER/ADMIN`
   - Admin 会话 Bearer：写操作需 `workflow.write`

## 存储

PostgreSQL 为权威（migration `000009_workflow_mcp.sql`）：

- `workflows` / `skill_packages` / `skill_package_files` / `knowledge_docs` / `knowledge_chunks`
- `employee_workflows` / `mcp_tokens`
- 知识检索：自定义 CJK bigram + ASCII 分词 → `to_tsvector('simple', ...)`（不依赖 zhparser）

无 DB 时回退内存 Store。

## 端点

| 类型 | 路径 |
|------|------|
| REST | `/api/workflow-mcp/*`、`/api/employees/{id}/workflows`、`/api/employees/{id}/mcp-tokens` |
| MCP JSON-RPC | `POST /mcp`（Streamable HTTP） |

MCP tools：`list/get/search/upsert/delete` 覆盖工作流、技能、知识；`check_local_skills` 接受客户端上报清单；`sync_skill(s)` 经 gRPC 下发到工作站。

## 运行时链路

1. Job 可带 `workflow_id`；Scheduler 校验授权，组装 `StartJobPayload`（工作流 YAML、技能包、指向 CP `/mcp` 的 MCPServerSpec）
2. Workstation 收到后：`skillsync` 落盘到 `~/.cursor/skills`，再 `session/new` 注入 `mcpServers`
3. 独立命令：`COMMAND_TYPE_SYNC_SKILLS`

## Admin Web

侧栏「工作流管理」`/workflows`：三 Tab（工作流 / 技能包 / 知识库）。员工详情：授权工作流、有效技能闭包、MCP Token 签发/吊销。

## 外部 MCP 身份凭证

在「MCP 服务与能力扩展管理」的身份凭证表单中选择认证方式与注入方式，再在员工详情中绑定对应服务和凭证。密钥保存在 SecretManager；`metadata` 仅存放非敏感的注入设置。

- `bearer`、`oauth`、`pat`：默认发送 `Authorization: Bearer <密钥>`。`oauth` 目前仅接收已取得的访问令牌，不自动登录或续期。
- `token`：发送 `Authorization: Token <密钥>`。
- `basic`：密钥填写 `用户名:密码`，运行时编码为 `Authorization: Basic <Base64>`。
- `api_key`：为兼容已有配置，未配置注入设置时仍使用 Bearer。需要 API Key 请求头时填写 `header_name`，如 `X-API-Key`，默认直接发送密钥原值。
- `metadata.header_prefix`：可指定其它认证前缀；显式空字符串表示不加前缀。
- STDIO 服务：选择环境变量注入并填写 `metadata.env_name`，密钥原值将传入该变量。此方式不能同时配置请求头。

例如，API Key 凭证的非敏感配置为：

```json
{"header_name":"X-API-Key","header_prefix":""}
```

调度时，绑定凭证会覆盖同名请求头（不区分大小写）或同名环境变量，其它配置保留。凭证必须处于 `active` 状态，且 `expires_at` 未到期、密钥可读取；校验失败会阻止任务下发并立即将任务标记为 `FAILED`，保存具体原因，不再自动重试。修正凭证后需提交新任务。员工忙、会话排队、工作站离线或资源不足属于正常等待条件，仍保留排队状态。自动续期暂未实现，需要手动更新密钥及有效期。此检查发生在任务下发前，不会主动撤销已运行会话中的上游凭证。

Cursor 通过 ACP `session/new.mcpServers` 接收配置，请求头和环境变量转换为 name/value 列表。Codex 通过 `thread/start.config.mcp_servers` 接收 HTTP 的 `http_headers` 或 STDIO 的 `command/args/env`，不会写入用户全局配置文件。Codex 不支持旧版 SSE 服务，需改为 Streamable HTTP。

## 导入

`POST /api/workflow-mcp/import`（multipart `file`）：上传 PersonalWorkMCP 的 `data/` zip（`workflows/*.yaml`、`skills/<name>/SKILL.md`、`knowledge/**/*.md`）。

## 导出给外部 MCP 客户端

在「MCP 服务与能力扩展管理 → 已注册的 MCP 服务」右侧点击「导出 MCP」，默认下载包含全部已注册服务的 `mcp.json`（顶层为 `mcpServers`），可合并到 Cursor 的 `.cursor/mcp.json`。切换为「Codex / TOML」后下载 `mcp.codex.toml`，将内容合并到 `~/.codex/config.toml`；不要覆盖已有的其它设置。

- 导出入口：`POST /api/mcp-servers/export`，JSON 请求 `{"format":"json","scope":"READ"}`；`format` 还支持 `codex`，`scope` 还支持 `WRITE`。导出全部服务需要 `workflow.read` 和 `workflow.grant` 的 ALL 范围；WRITE 还需管理员显式授予外部 MCP 写权限。沿用管理会话的 MFA 校验。
- 内置 `workflow-mcp` 每次导出独立签发 `EXTERNAL_USER` Token，绑定导出人，有效期 90 天，默认只读共享的工作流、技能和知识。可选择读写，不包含删除；每次调用检查用户状态及当前外部授权策略。该 Token 只适用于 MCP，不能当作管理后台登录会话。
- 「外部授权」可查看本人签发的 Token 元数据并吊销；列表不返回明文。对应 API 为 `GET /api/me/mcp-tokens` 和 `DELETE /api/me/mcp-tokens/{id}`。每份授权互不影响。旧列表路径 `/api/mcp-servers/export-tokens` 仍可用。
- 自定义服务导出注册的 URL、headers，或 STDIO command/args/env。可在服务编辑中选择「导出使用的身份凭证」；默认使用绑定中的本人唯一凭证。有多个本人凭证、只有他人凭证或凭证已失效时，整个导出失败并提示修正，不静默遗漏服务。显式选择「仅使用服务配置」可不附加保管库凭证。即使拥有全局管理权限，也不能导出他人拥有的保管库凭证。
- 第三方凭证权限、有效期和吊销由原服务控制，不受内置 MCP 的只读/90 天/吊销设置约束。下载文件包含连接凭证，仅交给获授权的使用者。导出响应禁止缓存，审计只记录导出人、格式、范围、数量和 Token ID。
- 地址优先使用 `AIE_MCP_PUBLIC_URL`，其次 `AIE_PUBLIC_SERVER_URL + /mcp`，否则使用当前浏览器访问地址。Admin Nginx 和开发代理已转发 `/mcp`。原来注册的第三方 URL 不会自动改写：本机地址、内网地址及 STDIO 命令仍需确保在目标电脑可用。
- 全部注册服务均会导出，已停用服务带 JSON `disabled: true` / TOML `enabled = false`，并提示检查客户端启用状态。Codex 格式遇到 SSE 服务会明确报错，不静默漏项；可切回 JSON 或将服务迁移到 HTTP。

该导出提供现有服务的客户端连接配置，内置 MCP 仍由 Control Plane 的 `/mcp` 提供服务。

## 用户自行创建外部授权

普通用户在「个人设置 → 外部 MCP 授权」填写客户端名称，选择工作流、工作采集器或两者，选择 JSON 或 TOML，点击「创建凭证并下载配置」。该入口只导出本人的内置 MCP 连接，不导出第三方服务凭证；默认只读，无需全局工作流管理权限。当前外部 MCP 连接使用个人 Bearer Token；采集器内部的个人飞书 OAuth 是另一个授权流程。

- `GET /api/me/mcp-policy`：读取本人可签发的权限。
- `POST /api/me/mcp-tokens`：请求 `{"label":"我的 Cursor","format":"json","scope":"READ"}`，创建独立的 90 天凭证并返回配置。`scope` 默认为 READ，WRITE 必须已有管理员授权。不能指定其他用户作为凭证主体。
- `GET /api/me/mcp-tokens` / `DELETE /api/me/mcp-tokens/{id}`：仅列出、吊销自己的外部凭证。明文只在创建响应中返回，数据库存储令牌哈希。
- 管理员在「用户管理 → 指定用户 → 外部 MCP 授权」授予或收回写权限。API 为 `GET/PUT /api/users/{id}/mcp-policy`，PUT 请求 `{"can_write":true}`。仅 ADMIN / SUPER_ADMIN 且具有全局 `user.update` 权限可修改；普通管理员不能修改超级管理员的策略。
- 权限存储在迁移 `000037_mcp_external_user_policies.sql` 创建的表中。没有记录即只读，管理员账号也没有隐式外部写权限。外部授权与后台角色独立，改变后台角色不会代替收回外部授权。
- WRITE 包含新增、修改、技能同步及索引重建，不包含删除；READ 凭证不会因账号获得写授权而自动升级。收回写权限后已有 WRITE 凭证退为只读；重新授予后，尚未吊销或过期的 WRITE 凭证恢复写入。禁用用户后，其所有外部凭证不能继续调用。
- 旧版 EXTERNAL_USER / ADMIN 凭证不再被接受，需重新创建 READ 或 WRITE 凭证。员工 Token 和旧 USER Token 继续沿用原授权规则。
- 自助签发、列表、吊销及管理员授权入口均保留会话及 MFA 检查，浏览器写操作保留来源校验。第三方 MCP 的权限仍需在对应第三方服务管理。

## 两个内置 MCP 的统一授权

迁移 `000038_work_collector.sql` 新增内置 `personal-work-collector`（`/mcp/collector`）。它与 `workflow-mcp`（`/mcp`）共用鉴权代码，每份 Token 使用独立 `service` audience，不能跨服务使用。旧 Token 默认仅属于工作流。

- 个人签发支持 `{"services":[{"service":"workflow-mcp","scope":"READ"},{"service":"personal-work-collector","scope":"WRITE"}]}`；不传 `services` 时兼容原工作流单服务请求。
- 管理员策略支持 `{"service":"personal-work-collector","can_write":true}`；不传 `service` 时修改原工作流策略。两个写权限独立更新，不互相覆盖。
- 全服务导出中，`scope` 选择工作流权限，`collector_scope` 选择采集器权限，后者默认 READ；每个内置服务分别签发 Token。
- 员工绑定采集器后下发独立的只读服务 Token；每次请求解析当前员工所有者、绑定状态及工具允许/拒绝列表。
- 收回写权限、吊销 Token、禁用用户、停用服务对相应 MCP 后续调用即时生效。采集器自动扫描单独由目标启用状态管理。
- 个人飞书身份、扫描与规则设置、迁移方法见 [PERSONAL_WORK_COLLECTOR.md](PERSONAL_WORK_COLLECTOR.md)。

## 环境变量

| 变量 | 说明 |
|------|------|
| `AIE_MCP_PUBLIC_URL` | 下发给工作站的 MCP URL（默认 `http://127.0.0.1{HTTPAddr}/mcp`） |
| `CURSOR_SKILLS_DIR` | 工作站技能安装目录（默认 `~/.cursor/skills`） |
