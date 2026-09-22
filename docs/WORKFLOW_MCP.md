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

## 导入

`POST /api/workflow-mcp/import`（multipart `file`）：上传 PersonalWorkMCP 的 `data/` zip（`workflows/*.yaml`、`skills/<name>/SKILL.md`、`knowledge/**/*.md`）。

## 环境变量

| 变量 | 说明 |
|------|------|
| `AIE_MCP_PUBLIC_URL` | 下发给工作站的 MCP URL（默认 `http://127.0.0.1{HTTPAddr}/mcp`） |
| `CURSOR_SKILLS_DIR` | 工作站技能安装目录（默认 `~/.cursor/skills`） |
