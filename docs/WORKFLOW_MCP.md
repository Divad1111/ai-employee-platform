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

调度时，绑定凭证会覆盖同名请求头（不区分大小写）或同名环境变量，其它配置保留。凭证必须处于 `active` 状态，且 `expires_at` 未到期、密钥可读取；失败时阻止本次任务下发并保留可重试状态。自动续期暂未实现，需要手动更新密钥及有效期。此检查发生在任务下发前，不会主动撤销已运行会话中的上游凭证。

Cursor 通过 ACP `session/new.mcpServers` 接收配置，请求头和环境变量转换为 name/value 列表。Codex 通过 `thread/start.config.mcp_servers` 接收 HTTP 的 `http_headers` 或 STDIO 的 `command/args/env`，不会写入用户全局配置文件。Codex 不支持旧版 SSE 服务，需改为 Streamable HTTP。

## 导入

`POST /api/workflow-mcp/import`（multipart `file`）：上传 PersonalWorkMCP 的 `data/` zip（`workflows/*.yaml`、`skills/<name>/SKILL.md`、`knowledge/**/*.md`）。

## 环境变量

| 变量 | 说明 |
|------|------|
| `AIE_MCP_PUBLIC_URL` | 下发给工作站的 MCP URL（默认 `http://127.0.0.1{HTTPAddr}/mcp`） |
| `CURSOR_SKILLS_DIR` | 工作站技能安装目录（默认 `~/.cursor/skills`） |
