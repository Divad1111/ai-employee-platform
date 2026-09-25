# AI Employee System：多用户、多数字员工、MCP 与身份凭证设计

> 版本：v1.0  
> 用途：直接交给 Codex 进行数据库、Center Server、Admin Web、Workstation Agent 的开发  
> 核心目标：支持多个用户共享同一 Workstation；每个用户拥有多个 Digital Employee；每个 Digital Employee 可以使用不同 MCP；同一个 MCP 可以为不同 Digital Employee 绑定不同身份凭证。

---

# 1. 设计目标

系统必须明确区分以下概念：

- **User**：真实的人类用户
- **Digital Employee**：属于某个 User 的数字员工
- **Workspace**：数字员工实际工作的目录和运行环境配置
- **Workstation**：实际执行任务的 Windows / macOS / Linux 机器
- **MCP Server**：数字员工可以调用的能力提供方
- **Credential**：访问某个 MCP 时使用的身份认证凭证
- **MCP Binding**：Digital Employee 与 MCP Server、Credential、Tool Permission 之间的绑定关系
- **Session**：一次具体的数字员工执行会话
- **Task**：一次需要数字员工执行的工作任务

核心原则：

```text
User
  │
  └── owns
       ↓
Digital Employee
  │
  ├── Workspace ─────────────→ Workstation
  │
  └── MCP Binding
          │
          ├── MCP Server
          ├── Credential
          └── Tool Permission
```

**绝对禁止把 Workstation 当成 User 身份。**

---

# 2. 核心身份模型

## 2.1 User

User 表示真实人类。

示例：

```text
User: 张三
User ID: U001
```

User 可以拥有多个 Digital Employee。

```text
张三
├── 程序员小张
├── 产品小张
└── 运维小张
```

---

## 2.2 Digital Employee

Digital Employee 是真正执行任务的 AI 员工。

属性：

```text
id
owner_user_id
name
description
status
default_runtime
created_at
updated_at
```

例如：

```text
Employee:
    id = E001
    owner_user_id = U001
    name = 程序员小张
```

Digital Employee 必须拥有独立的逻辑身份。

---

# 3. Workspace

Workspace 表示某个 Digital Employee 的实际工作环境。

关系：

```text
Digital Employee
      │
      └── Workspace
              │
              └── Workstation
```

建议字段：

```text
Workspace
--------------------------------
id
employee_id
workstation_id
path
runtime_type
runtime_config
status
created_at
updated_at
```

例如：

```text
Employee:
    程序员小张

Workspace:
    workstation_id = WS001
    path = D:\AI\employees\zhangsan\coder
```

Workspace 是 Digital Employee 的工作目录，不是 User 的目录。

---

# 4. Workstation

Workstation 是实际运行 Workstation Agent 的机器。

它只是计算节点，不代表任何用户。

例如：

```text
Workstation-01
│
├── 张三 / 程序员小张
├── 李四 / 程序员小李
└── 王五 / 产品小王
```

Workstation 不应该保存：

```text
当前登录用户
默认 User Token
默认 Feishu Token
默认 MCP Token
```

Workstation 只负责：

- 接收 Center Server 分发的任务
- 根据任务指定的 Employee / Workspace 执行
- 启动 Cursor / Codex / 其他 Agent Runtime
- 管理独立 Session
- 调用 MCP
- 返回执行结果
- 上报状态和执行统计

---

# 5. MCP Server

MCP Server 表示一个能力提供方。

例如：

```text
GitHub MCP
Jira MCP
Feishu MCP
Figma MCP
Sentry MCP
```

建议字段：

```text
MCPServer
--------------------------------
id
name
description
server_type
endpoint
transport
config
status
created_at
updated_at
```

MCP Server 本身不保存某个具体用户的 OAuth Token。

例如：

```text
GitHub MCP
```

只是能力：

```text
search_code
get_repository
create_issue
create_pull_request
...
```

---

# 6. Credential

Credential 表示访问外部系统时使用的身份认证凭证。

这是本设计中最重要的实体之一。

MCP Server 与 Credential 必须分离。

例如：

```text
GitHub MCP
    │
    ├── 张三 GitHub Credential
    ├── 李四 GitHub Credential
    └── 公司 GitHub Bot Credential
```

Credential 可以属于：

```text
USER
ORGANIZATION
SERVICE_ACCOUNT
```

建议字段：

```text
Credential
--------------------------------
id
owner_type
owner_id
provider
auth_type
credential_name
encrypted_secret
refresh_token_encrypted
expires_at
metadata
status
created_at
updated_at
```

注意：

- Token / Secret 必须加密存储
- 不允许明文存储
- API 返回时不得返回 Secret
- 日志不得打印完整 Token
- UI 只能显示脱敏后的身份信息
- 支持 OAuth Token、API Key、PAT、Client Credential 等不同认证方式

---

# 7. Employee MCP Binding

这是整个 MCP 权限模型的核心。

Digital Employee 不直接“拥有 MCP Token”。

而是通过：

```text
EmployeeMCPBinding
```

关联：

```text
Digital Employee
       │
       └── MCP Binding
              ├── MCP Server
              ├── Credential
              └── Tool Permission
```

建议字段：

```text
EmployeeMCPBinding
--------------------------------
id
employee_id
mcp_server_id
credential_id
enabled
allowed_tools
denied_tools
config
created_at
updated_at
```

约束：

```text
employee_id
    +
mcp_server_id
```

可以作为唯一业务关系。

如果未来需要同一个 Employee 对同一个 MCP 同时使用多个身份，则允许多条 Binding，但必须增加明确的 binding_name / purpose。

---

# 8. MCP 权限与身份必须分开

必须明确：

> “能不能使用 MCP” 和 “以谁的身份使用 MCP” 是两个问题。

例如：

```text
程序员小张
```

配置：

```text
GitHub MCP
    enabled = true
    credential = 张三 GitHub
```

而：

```text
程序员小李
```

配置：

```text
GitHub MCP
    enabled = true
    credential = 李四 GitHub
```

两人：

```text
使用同一个 GitHub MCP
```

但是：

```text
使用不同 GitHub 身份
```

---

# 9. Tool Permission

MCP Binding 还应该支持工具级权限。

例如：

```text
GitHub MCP

允许：
- search_code
- get_repository
- get_issue
- create_issue

禁止：
- delete_repository
- delete_branch
- merge_pull_request
```

建议支持：

```text
allowed_tools
denied_tools
```

权限判断建议：

```text
if binding.enabled == false:
    DENY

if tool in denied_tools:
    DENY

if allowed_tools is not empty and tool not in allowed_tools:
    DENY

ALLOW
```

后续可以扩展为 RBAC / Policy Engine，但第一版不需要过度设计。

---

# 10. Credential Owner

Credential 不应该强制绑定 User。

建议：

```text
owner_type
owner_id
```

支持：

```text
USER
ORGANIZATION
SERVICE_ACCOUNT
```

示例：

```text
Credential #001
owner_type = USER
owner_id = U001
provider = github
name = 张三 GitHub
```

```text
Credential #002
owner_type = ORGANIZATION
owner_id = ORG001
provider = jira
name = 公司 Jira Bot
```

```text
Credential #003
owner_type = SERVICE_ACCOUNT
owner_id = SA001
provider = github
name = CI Bot
```

这样公司级共享账号可以被多个 Digital Employee 使用。

---

# 11. 完整数据关系

推荐最终关系：

```text
User
 │
 └── 1:N ── DigitalEmployee
                 │
                 ├── 1:N ── Workspace
                 │                │
                 │                └── N:1 ── Workstation
                 │
                 └── 1:N ── EmployeeMCPBinding
                                  │
                                  ├── N:1 ── MCPServer
                                  │
                                  └── N:1 ── Credential
```

完整结构：

```text
                    ┌───────────────┐
                    │     User      │
                    └───────┬───────┘
                            │
                         owns
                            │
                            ▼
                  ┌──────────────────┐
                  │ Digital Employee │
                  └────────┬─────────┘
                           │
                ┌──────────┴───────────┐
                │                      │
                ▼                      ▼
          ┌───────────┐       ┌─────────────────┐
          │ Workspace │       │  MCP Binding    │
          └─────┬─────┘       └────────┬────────┘
                │                      │
                ▼                 ┌────┴─────┐
          ┌────────────┐           │          │
          │ Workstation│      MCP Server  Credential
          └────────────┘
```

---

# 12. 飞书身份

飞书身份属于 User，而不是 Workstation。

建议增加：

```text
Identity
--------------------------------
id
user_id
provider
tenant_key
provider_user_id
open_id
union_id
identity_metadata
created_at
updated_at
```

例如：

```text
User = 张三

Feishu Identity
    provider = feishu
    open_id = xxx
    union_id = xxx
```

飞书 OAuth Token / User Access Token 应作为 Credential 保存，而不是保存到 Workstation。

---

# 13. 飞书消息处理流程

用户在飞书发送：

```text
@程序员小张
帮我修复 XXX Bug
```

Center Server：

```text
Feishu Event
    ↓
识别 sender
    ↓
Identity Mapping
    ↓
User = 张三
    ↓
识别 mentioned Digital Employee
    ↓
Employee = 程序员小张
    ↓
找到 Employee Workspace
    ↓
找到 Workspace 所属 Workstation
    ↓
创建 Task
    ↓
创建 Session
    ↓
发送给 Workstation
```

任务执行上下文必须至少包含：

```json
{
  "user_id": "U001",
  "employee_id": "E001",
  "workspace_id": "W001",
  "workstation_id": "WS001",
  "session_id": "S001",
  "task_id": "T001"
}
```

Workstation 不允许根据本机当前登录用户推断 User。

---

# 14. MCP 调用流程

假设：

```text
User = 张三
Employee = 程序员小张
MCP = GitHub
Credential = 张三 GitHub
```

调用：

```text
Employee Session
       ↓
EmployeeMCPBinding
       ↓
检查 enabled
       ↓
检查 Tool Permission
       ↓
获取 Credential
       ↓
Credential Provider
       ↓
获取短期 Access Token
       ↓
调用 GitHub MCP
```

逻辑：

```text
Task
 ↓
Session
 ↓
Employee
 ↓
MCP Binding
 ↓
MCP Server
 ↓
Credential
 ↓
Access Token
 ↓
MCP Tool
```

---

# 15. Workstation 不应该决定身份

错误设计：

```text
Workstation
    ↓
当前用户 = 张三
    ↓
所有 Employee 都使用张三 Token
```

正确设计：

```text
Task
 ↓
Employee
 ↓
MCP Binding
 ↓
Credential
```

Workstation 只负责：

```text
Where to execute
```

而 Center Server / Authorization Layer 决定：

```text
Who is executing
What can be used
Which credential is used
```

---

# 16. 多用户共享 Workstation

例如：

```text
Workstation WS001
│
├── Workspace W001
│    ├── User = 张三
│    ├── Employee = 程序员小张
│    └── Path = D:\AI\zhangsan\coder
│
├── Workspace W002
│    ├── User = 李四
│    ├── Employee = 程序员小李
│    └── Path = D:\AI\lisi\coder
│
└── Workspace W003
     ├── User = 王五
     ├── Employee = 产品小王
     └── Path = D:\AI\wangwu\pm
```

三个 Employee 可以同时运行。

每个 Employee 使用独立：

- Workspace
- Agent Session
- MCP Binding
- Credential Context
- Task Context

---

# 17. Workspace 隔离

Workstation Agent 必须确保：

```text
Employee A
    ≠
Employee B
```

工作目录不能混用。

例如：

```text
D:\AI\employees\
├── zhangsan\
│   └── coder\
├── lisi\
│   └── coder\
└── wangwu\
    └── pm\
```

每次启动 Cursor / Codex 时必须明确传入 Employee Workspace。

禁止使用：

```text
Workstation 默认目录
Workstation 当前目录
用户 Home Directory
```

作为 Employee 工作目录。

---

# 18. Admin-Web 设计

## 18.1 Digital Employee 页面

页面：

```text
数字员工
```

显示：

```text
名称
所属用户
状态
当前 Workstation
Workspace
Runtime
MCP 数量
最近任务
```

详情页：

```text
┌──────────────────────────────────────────┐
│ 程序员小张                               │
├──────────────────────────────────────────┤
│ 所属用户：张三                           │
│ Workstation：WS001                       │
│ Workspace：D:\AI\zhangsan\coder          │
│ Runtime：Cursor                          │
├──────────────────────────────────────────┤
│ MCP                                      │
│                                          │
│ GitHub     已启用   张三 GitHub          │
│ Jira       已启用   公司 Jira Bot        │
│ Feishu     已启用   张三 Feishu          │
│ Figma      未启用                        │
│                                          │
│                 [+ 添加 MCP]             │
└──────────────────────────────────────────┘
```

---

# 19. 添加 MCP 流程

Admin 点击：

```text
+ 添加 MCP
```

步骤：

```text
Step 1
选择 MCP Server

Step 2
选择 Credential

Step 3
配置 Tool Permission

Step 4
配置 MCP 专属参数

Step 5
保存 Binding
```

例如：

```text
MCP:
    GitHub MCP

Credential:
    张三 GitHub

Tools:
    ☑ search_code
    ☑ get_repository
    ☑ get_issue
    ☑ create_issue
    ☐ delete_repository
    ☐ merge_pull_request
```

保存后：

```text
EmployeeMCPBinding
```

立即生效。

---

# 20. Credential 管理页面

Admin-Web 应提供：

```text
凭证管理
```

列表：

```text
名称
Provider
Owner
Auth Type
状态
过期时间
使用中的 Employee 数量
```

例如：

```text
张三 GitHub
GitHub
USER / 张三
OAuth
正常
2027-01-01
2

公司 Jira Bot
Jira
ORGANIZATION / 公司
OAuth
正常
2027-06-01
15
```

Token 不显示完整值。

只显示：

```text
ghp_************9x82
```

或：

```text
已配置
```

---

# 21. Credential OAuth 流程

推荐：

```text
Admin-Web
    ↓
创建 Credential
    ↓
跳转 OAuth Provider
    ↓
用户授权
    ↓
Center Server Callback
    ↓
获取 Access Token / Refresh Token
    ↓
加密保存
    ↓
Credential 创建完成
```

不要让：

```text
Workstation
```

承担 OAuth Callback Server。

OAuth 统一由 Center Server 处理更容易管理。

如果某 MCP 必须在本地完成 OAuth，则允许特殊的 Local Authorization Flow，但最终仍然要归档为 Credential。

---

# 22. Credential 加密

必须支持：

```text
Encryption at Rest
```

建议：

```text
Center Server
    │
    └── Credential Store
            │
            └── KMS / Master Key
```

数据库保存：

```text
ciphertext
```

而不是：

```text
access_token
refresh_token
```

日志禁止出现：

```text
Authorization: Bearer xxxxx
```

所有日志必须脱敏。

---

# 23. Session

一次具体执行需要独立 Session。

建议：

```text
Session
--------------------------------
id
task_id
employee_id
workspace_id
workstation_id
runtime
status
started_at
ended_at
token_usage
metadata
```

Session 不应该直接保存长期 Credential。

Session 只引用：

```text
EmployeeMCPBinding
```

或者在运行时生成短期 Credential Context。

---

# 24. Task

Task 至少记录：

```text
Task
--------------------------------
id
user_id
employee_id
workspace_id
workstation_id
session_id
source
source_message_id
status
prompt
result
token_usage
created_at
started_at
finished_at
```

这样以后统计：

```text
User
Employee
Workstation
Task
Token Usage
```

都可以直接关联。

---

# 25. Token 消耗统计

不需要建立独立的“Token 上传服务”。

每个 Task / Session 保存：

```text
input_tokens
output_tokens
total_tokens
```

例如：

```json
{
  "input_tokens": 12000,
  "output_tokens": 3500,
  "total_tokens": 15500
}
```

然后后台统计：

```text
User
 ↓
Employee
 ↓
Task
 ↓
Token Usage
```

可以统计：

```text
今日消耗
本月消耗
Employee 消耗
Workstation 消耗
Runtime 消耗
Model 消耗
```

如果底层 Cursor / Codex 无法可靠提供真实 Token，则必须明确区分：

```text
reported_usage
estimated_usage
```

不要伪装成真实 Token。

---

# 26. 审计日志

所有 MCP 操作建议记录：

```text
AuditLog
--------------------------------
id
user_id
employee_id
workspace_id
workstation_id
session_id
mcp_server_id
credential_id
tool_name
action
status
error_code
timestamp
```

例如：

```text
2026-09-25 21:45:03

User:
U001 / 张三

Employee:
E003 / 程序员小张

Workstation:
WS002

MCP:
GitHub

Credential:
C012 / 张三 GitHub

Tool:
search_code

Result:
SUCCESS
```

这样可以回答：

> 谁让哪个数字员工，以什么身份，在什么工作站执行了什么 MCP 操作？

---

# 27. 安全边界

必须满足：

## 27.1 Workstation 不能越权

Workstation Agent 收到：

```text
employee_id = E001
```

只能执行：

```text
E001
```

对应的 Workspace。

不能自行修改为：

```text
E002
```

## 27.2 Workstation 不能自行选择 Credential

Credential 必须来自：

```text
Center Server Authorization Context
```

不能让客户端任意传：

```text
credential_id = C999
```

然后 Workstation 直接使用。

Center Server 必须验证：

```text
Employee
    ↓
是否允许使用 MCP
    ↓
是否允许使用 Credential
```

## 27.3 Credential 最小权限

Credential 本身也应该遵循外部系统的最小权限原则。

---

# 28. 推荐的授权检查

一次 MCP 调用：

```text
User
 ↓
Employee
 ↓
Binding
 ↓
MCP
 ↓
Credential
 ↓
Tool
```

必须依次检查：

```text
1. User 是否有效
2. Employee 是否属于 User
3. Employee 是否启用
4. Employee 是否允许使用该 MCP
5. Binding 是否启用
6. Credential 是否有效
7. Credential 是否允许被该 Employee 使用
8. Credential 是否过期
9. Tool 是否在允许列表
10. MCP Server 是否可用
```

全部通过后才能执行。

---

# 29. API 设计建议

## Digital Employee

```http
GET    /api/employees
POST   /api/employees
GET    /api/employees/:id
PUT    /api/employees/:id
DELETE /api/employees/:id
```

## MCP

```http
GET    /api/mcp-servers
POST   /api/mcp-servers
GET    /api/mcp-servers/:id
PUT    /api/mcp-servers/:id
DELETE /api/mcp-servers/:id
```

## Employee MCP Binding

```http
GET    /api/employees/:id/mcp-bindings
POST   /api/employees/:id/mcp-bindings
PUT    /api/employees/:id/mcp-bindings/:bindingId
DELETE /api/employees/:id/mcp-bindings/:bindingId
```

## Credential

```http
GET    /api/credentials
POST   /api/credentials
GET    /api/credentials/:id
PUT    /api/credentials/:id
DELETE /api/credentials/:id
POST   /api/credentials/:id/oauth/start
GET    /api/credentials/:id/oauth/callback
POST   /api/credentials/:id/test
```

---

# 30. Workstation Agent 接口

Center Server → Workstation：

```json
{
  "task_id": "T001",
  "employee_id": "E001",
  "workspace_id": "W001",
  "workstation_id": "WS001",
  "session_id": "S001",
  "runtime": "cursor",
  "prompt": "修复 XXX Bug"
}
```

Workstation Agent 必须根据：

```text
workspace_id
```

找到本地：

```text
workspace path
```

并启动对应 Runtime。

---

# 31. Workstation 本地数据

Workstation 本地只保存必要的运行信息：

```text
workstation identity
workspace mapping
runtime status
session state
temporary credential context
logs
```

不要保存完整的公司级 User 数据。

更不要把：

```text
所有 User Token
所有 Employee Token
```

长期复制到 Workstation。

---

# 32. 推荐 Credential 下发模式

优先：

```text
Center Server
      ↓
授权检查
      ↓
生成短期 Credential Context
      ↓
Workstation
      ↓
MCP Call
```

而不是：

```text
Center Server
      ↓
把长期 Refresh Token
      ↓
永久发送到 Workstation
```

如果 MCP 必须在 Workstation 本地完成认证，也应该：

- 加密保存
- 限定 Employee / Binding
- 设置过期时间
- 支持撤销
- 不允许其他 Employee 读取

---

# 33. 一个完整例子

公司有：

```text
User:
张三
李四
```

数字员工：

```text
张三
└── 程序员小张

李四
└── 程序员小李
```

Workstation：

```text
WS001
```

Workspace：

```text
W001 → 张三 / 程序员小张
W002 → 李四 / 程序员小李
```

MCP：

```text
GitHub MCP
Jira MCP
Feishu MCP
```

Credential：

```text
C001 = 张三 GitHub
C002 = 李四 GitHub
C003 = 公司 Jira Bot
C004 = 张三 Feishu
C005 = 李四 Feishu
```

Binding：

```text
E001 / 程序员小张
    GitHub → C001
    Jira   → C003
    Feishu → C004

E002 / 程序员小李
    GitHub → C002
    Jira   → C003
    Feishu → C005
```

最终：

```text
                 WS001
                   │
          ┌────────┴────────┐
          │                 │
      程序员小张          程序员小李
          │                 │
       W001              W002
          │                 │
       ┌──┼──┐           ┌──┼──┐
       │  │  │           │  │  │
     Git Jira Feishu   Git Jira Feishu
       │   │    │        │   │    │
      C1  C3   C4       C2  C3   C5
       │   │    │        │   │    │
      张三 公司  张三     李四 公司  李四
```

这正是系统需要支持的模型。

---

# 34. 第一版实现优先级

Codex 开发时建议按照下面顺序实现：

## Phase 1：核心实体

实现：

```text
User
DigitalEmployee
Workstation
Workspace
MCPServer
Credential
EmployeeMCPBinding
Session
Task
AuditLog
```

## Phase 2：Admin-Web

实现：

```text
用户管理
数字员工管理
Workstation 管理
Workspace 管理
MCP 管理
Credential 管理
Employee MCP Binding 管理
```

## Phase 3：授权

实现：

```text
User Identity
OAuth
Credential Encryption
Credential Test
MCP Permission
Tool Permission
```

## Phase 4：Workstation

实现：

```text
Workspace Mapping
Task Dispatch
Session
Cursor Runtime
Codex Runtime
MCP Runtime
```

## Phase 5：飞书

实现：

```text
Feishu Identity Mapping
Feishu Event
Mention Employee
Task Creation
Task Result
```

## Phase 6：审计和统计

实现：

```text
Task Timeline
MCP Audit
Token Usage
Employee Usage
Workstation Usage
Credential Usage
```

---

# 35. 最终架构原则

整个系统必须遵守以下原则：

### 原则 1

```text
Workstation ≠ User
```

Workstation 是计算节点。

### 原则 2

```text
Digital Employee ≠ User
```

Digital Employee 是 AI 身份，User 是真实用户。

### 原则 3

```text
MCP Permission ≠ Credential
```

Permission 决定“能不能用”。

Credential 决定“以谁的身份用”。

### 原则 4

```text
MCP Server ≠ Credential
```

MCP Server 是能力。

Credential 是身份。

### 原则 5

```text
Workspace ≠ Workstation
```

Workspace 是 Employee 的工作环境。

Workstation 是 Workspace 的运行节点。

### 原则 6

```text
Credential ≠ Session
```

Credential 是长期/可刷新身份凭证。

Session 是一次具体执行上下文。

### 原则 7

任何一次 MCP 调用都必须能够追溯：

```text
Who
  ↓
User

Which AI
  ↓
Digital Employee

Where
  ↓
Workspace / Workstation

What capability
  ↓
MCP Server / Tool

Using which identity
  ↓
Credential

Which execution
  ↓
Session / Task
```

---

# 36. 最终推荐模型

最终系统建议固定为：

```text
                           ┌──────────────┐
                           │     User     │
                           └──────┬───────┘
                                  │
                                  │ owns
                                  ▼
                       ┌────────────────────┐
                       │ Digital Employee   │
                       └─────────┬──────────┘
                                 │
                    ┌────────────┴────────────┐
                    │                         │
                    ▼                         ▼
             ┌─────────────┐          ┌─────────────────┐
             │  Workspace  │          │  MCP Binding    │
             └──────┬──────┘          └────────┬────────┘
                    │                          │
                    ▼                     ┌────┴────┐
             ┌─────────────┐               │         │
             │ Workstation │          MCP Server Credential
             └─────────────┘               │         │
                                           │         │
                                           └────┬────┘
                                                ▼
                                           MCP Tool Call
```

**核心一句话：**

> **User 决定“是谁”，Digital Employee 决定“哪个 AI 在工作”，Workspace 决定“在哪个工作环境工作”，Workstation 决定“在哪台机器执行”，MCP Binding 决定“这个数字员工能使用什么 MCP”，Credential 决定“调用 MCP 时以谁的身份访问”。**

这套模型作为后续 Center Server、Admin-Web、Workstation Agent、飞书集成、MCP 集成以及权限系统的统一基础模型。
