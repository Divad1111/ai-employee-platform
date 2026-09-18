# AI Employee System — 完整技术设计与开发需求

> **用途：直接交给 Codex / Cursor 作为项目总体设计与开发依据**
>
> 本文档整合此前整个 AI Employee 系统设计讨论，覆盖 **Control Plane（中心服务器）+ Admin 管理后台 + Feishu + Employee + Job + Workstation + Cursor/Codex + ACP + MCP + 安全 + 数据库 + 部署 + 开发计划**。
>
> 核心目标：先定义完整系统边界和协议，再按 V1 → V2 → V3 分阶段实现，避免把 Workstation 单独实现成一个孤立执行器。

---

# 1. 项目定位

AI Employee System 是一个由中心服务器统一管理多个 AI Employee，并通过 Workstation 将 AI Agent 真正部署到物理计算机上的平台。

核心理念：

```text
                         User
                          │
                       Feishu
                          │
                          ▼
                  ┌─────────────────┐
                  │  Control Plane  │
                  │   中心服务器     │
                  └────────┬────────┘
                           │
                  管理 / 调度 / 消息
                           │
             ┌─────────────┼─────────────┐
             │             │             │
          Employee A    Employee B    Employee C
             │             │             │
       Workstation A  Workstation A  Workstation B
             │             │             │
          Cursor         Codex        Cursor
             │             │             │
            ACP           ACP          ACP
             │             │             │
          Workspace     Workspace    Workspace
```

核心边界：

```text
Control Plane
    ↓
What / Who / When
业务、身份、调度、管理

Workstation
    ↓
Where / How
执行位置、进程、Workspace、Agent Runtime

ACP
    ↓
AI Agent Communication
AI Agent 的标准通信

MCP
    ↓
Tools / External Context
Agent 使用外部工具和上下文

Workspace
    ↓
代码、文件、项目
```

---

# 2. 系统核心概念

## 2.1 Employee

Employee 是平台中的一个长期存在的 AI 员工身份，而不是一个 Cursor 进程。

Employee 包含：

- Employee ID
- 名称
- 描述
- 职责
- Skill
- Knowledge
- Feishu 身份
- 默认 Provider
- Workspace
- Permission Profile
- Runtime Policy
- Session Policy
- 所属 Workstation
- 状态

重要原则：

> Employee ≠ Cursor 进程。

一个 Employee 可以先没有运行中的 Session。

---

# 3. Employee / Workstation / Session / Job 关系

```text
Employee
   │
   ├── Workspace
   │
   ├── Provider
   │
   ├── Session
   │
   └── Job
```

进一步：

```text
Employee
   │
   └── Workstation
          │
          └── Runtime
                 │
                 └── Session
                        │
                        ├── Job A
                        ├── Job B
                        └── Job C
```

不要设计成：

```text
Employee = Cursor Process
```

而应该：

```text
Employee = AI 员工身份
Session  = AI Agent Runtime 会话
Job      = 一次具体工作
```

---

# 4. Control Plane 中心服务器

## 4.1 定位

Control Plane 是整个系统的大脑，负责：

- 用户
- Employee
- Workstation
- Feishu
- Job
- Message
- Event
- Scheduler
- Permission
- Approval
- Secret
- Artifact
- Skill
- Knowledge
- Audit Log
- System Configuration
- Admin API
- Workstation 通信

它不负责直接执行 AI Agent。

核心原则：

> Control Plane 决定“谁在什么时候做什么”。

Workstation 决定：

> “在哪里、如何执行”。

---

# 5. Control Plane 总体架构

推荐：

```text
┌──────────────────────────────────────────────────────────────┐
│                       Control Plane                          │
│                          Docker                              │
│                                                              │
│  ┌────────────────────────────────────────────────────────┐  │
│  │                    API Gateway                         │  │
│  └──────────────────────────┬─────────────────────────────┘  │
│                             │                                │
│       ┌─────────────────────┼──────────────────────────┐     │
│       │                     │                          │     │
│       ▼                     ▼                          ▼     │
│  Admin API             Feishu API                Worker API  │
│       │                     │                          │     │
│       └─────────────────────┼──────────────────────────┘     │
│                             ▼                                │
│                    ┌─────────────────┐                       │
│                    │ Application Core │                       │
│                    └────────┬────────┘                       │
│                             │                                │
│       ┌──────────┬──────────┼──────────┬──────────┐          │
│       ▼          ▼          ▼          ▼          ▼          │
│   Employee    Workstation   Job      Scheduler  Approval     │
│   Manager     Manager       Manager              Center      │
│                                                              │
│   Message Bus   Event Bus   Permission   Secret   Artifact   │
│                                                              │
│                         PostgreSQL                           │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

---

# 6. Control Plane 模块

建议拆分：

```text
control-plane/
├── api/
├── auth/
├── user/
├── employee/
├── workstation/
├── workspace/
├── job/
├── message/
├── event/
├── scheduler/
├── feishu/
├── permission/
├── approval/
├── secret/
├── artifact/
├── skill/
├── knowledge/
├── audit/
├── notification/
├── registry/
├── config/
└── database/
```

---

# 7. User / Admin 身份系统

系统至少存在：

```text
User
Employee
Workstation
```

三种身份不能混淆。

## User

人类管理员 / 操作者。

负责：

- 登录 Admin
- 管理 Employee
- 管理 Workstation
- 配置 Feishu
- 查看 Job
- Approval
- 查看 Audit Log

## Employee

AI 身份。

## Workstation

执行节点身份。

---

# 8. Admin 管理后台

Admin Web 是整个系统的管理入口。

建议：

```text
Admin
│
├── Dashboard
├── Employees
├── Workstations
├── Jobs
├── Sessions
├── Feishu
├── Skills
├── Knowledge
├── Permissions
├── Approvals
├── Secrets
├── Artifacts
├── Audit Logs
├── System
│
└── Settings
```

---

# 9. Admin Dashboard

Dashboard 展示系统实时状态：

```text
┌────────────────────────────────────────────┐
│ AI Employee Control Center                │
├────────────────────────────────────────────┤
│                                            │
│ Employees       Workstations       Jobs    │
│    12                5              37     │
│                                            │
│ Online            Busy             Errors  │
│    9                4                1     │
│                                            │
├────────────────────────────────────────────┤
│ Active Jobs                                │
│                                            │
│ JOB-1001  Unity Developer   RUNNING        │
│ JOB-1002  QA Employee       WAITING        │
│ JOB-1003  Build Employee    SUCCESS        │
│                                            │
├────────────────────────────────────────────┤
│ Workstations                               │
│                                            │
│ WS-001  ONLINE   CPU 32%  RAM 48%          │
│ WS-002  ONLINE   CPU 71%  RAM 82%          │
│ WS-003  OFFLINE                             │
│                                            │
└────────────────────────────────────────────┘
```

Dashboard 不应该只做静态统计，应支持实时刷新。

---

# 10. Employee 管理页面

Employee 列表：

```text
ID
Name
Role
Status
Provider
Workstation
Workspace
Active Session
Last Activity
```

Employee 详情：

```text
Employee
├── Basic Info
├── Feishu
├── Skills
├── Knowledge
├── Runtime
├── Workspace
├── Permission
├── Sessions
├── Jobs
├── Artifacts
├── Logs
└── Audit
```

管理员可以：

- 创建 Employee
- 修改 Employee
- 停用 Employee
- 删除 Employee
- 绑定 Workstation
- 绑定 Workspace
- 配置 Provider
- 配置权限
- 配置 Skill
- 配置 Knowledge
- 配置 Feishu

---

# 11. Employee 与 Feishu

目标：

> 所有 AI Employee 都可以进入飞书群。

Employee 有唯一 ID。

例如：

```text
EMP-UNITY
EMP-QA
EMP-BUILD
EMP-RESEARCH
```

在不同群：

```text
群 A
User @EMP-UNITY

群 B
User @EMP-UNITY

群 C
User @EMP-UNITY
```

Employee 都应该能识别自己的 @。

---

# 12. Feishu 消息流

```text
User
 │
 │ @EMP-UNITY 帮我检查这个 Unity Bug
 ▼
Feishu
 │
 ▼
Feishu Bot / Webhook
 │
 ▼
Control Plane
 │
 ├── Identity Resolver
 │
 ├── Employee Resolver
 │
 ├── Permission Check
 │
 └── Job Creator
 │
 ▼
JOB-1001
 │
 ▼
Scheduler
 │
 ▼
Workstation
 │
 ▼
Employee Runtime
 │
 ▼
Cursor / Codex
 │
 ▼
ACP
```

---

# 13. Employee 间协作

Employee 可以互相 @。

例如：

```text
User
 ↓
@UNITY-DEVELOPER 开发功能

UNITY-DEVELOPER
 ↓
@QA-EMPLOYEE 请测试

QA-EMPLOYEE
 ↓
执行测试

QA-EMPLOYEE
 ↓
@UNITY-DEVELOPER 测试发现问题
```

Control Plane 必须支持：

```text
Employee → Employee Message
```

不能只支持：

```text
User → Employee
```

---

# 14. Message Bus

消息是系统的核心对象之一。

建议统一 Message：

```text
Message
├── message_id
├── conversation_id
├── sender_type
├── sender_id
├── receiver_type
├── receiver_id
├── content
├── attachments
├── reply_to
├── created_at
└── metadata
```

sender_type：

```text
USER
EMPLOYEE
SYSTEM
```

receiver_type：

```text
EMPLOYEE
USER
GROUP
SYSTEM
```

---

# 15. Job 系统

Job 表示一次具体工作。

例如：

```text
JOB-1001

Employee:
EMP-UNITY

Workspace:
UNITY-GAME

Provider:
Cursor

Task:
修复 PlayerController 空引用问题
```

Job 状态：

```text
CREATED
   ↓
QUEUED
   ↓
ASSIGNED
   ↓
STARTING
   ↓
RUNNING
   ↓
SUCCESS
```

异常：

```text
FAILED
CANCELLED
TIMEOUT
BLOCKED
WAITING_APPROVAL
UNKNOWN
```

---

# 16. Job 与 Session

不要：

```text
Job = Session
```

应该：

```text
Employee
   │
   └── Session
         │
         ├── Job A
         ├── Job B
         └── Job C
```

这样 Session 可以复用：

```text
Job A
 ↓
Cursor Session 1

Job A 完成

Job B
 ↓
复用 Cursor Session 1
```

可以保留：

- Agent Context
- MCP 状态
- 工作上下文
- 已理解的代码结构

---

# 17. Scheduler

Scheduler 负责：

```text
Job
 ↓
寻找合适 Employee
 ↓
寻找 Workstation
 ↓
检查资源
 ↓
检查 Provider
 ↓
检查 Workspace
 ↓
检查权限
 ↓
分配
```

选择因素：

- Employee 是否可用
- Workstation 是否在线
- Workspace 是否存在
- Provider 是否安装
- CPU
- Memory
- 当前 Session
- 并发限制
- Permission Profile

---

# 18. Workstation Manager

Control Plane 保存：

```text
Workstation
├── ID
├── Name
├── OS
├── Arch
├── Version
├── IP / Network
├── Status
├── CPU
├── Memory
├── Disk
├── Certificate
├── Employees
├── Last Heartbeat
└── Capabilities
```

状态：

```text
PROVISIONING
ONLINE
BUSY
DRAINING
OFFLINE
ERROR
```

---

# 19. Workstation 注册

CLI：

```bash
aew register \
  --server https://worker.example.com \
  --token xxxxx
```

流程：

```text
aew
 ↓
生成 Workstation ID
 ↓
生成私钥
 ↓
生成 CSR
 ↓
Enrollment Token
 ↓
Control Plane 验证
 ↓
签发 Client Certificate
 ↓
保存 Identity
 ↓
建立 mTLS
```

Workstation 之后不再依赖 Enrollment Token。

---

# 20. Workstation 通信安全

Workstation ↔ Control Plane：

```text
gRPC
 ↓
TLS 1.3
 ↓
mTLS
```

双方验证：

```text
Client Certificate
Server Certificate
```

每台 Workstation 都拥有唯一：

```text
workstation_id
private_key
client_certificate
```

---

# 21. 防 Replay

所有 Command / Event 都需要具备防重放能力。

建议字段：

```text
message_id
sequence
timestamp
nonce
```

Control Plane 保存已处理 sequence。

规则：

```text
sequence <= last_sequence
        ↓
      REJECT
```

timestamp 超过允许窗口：

```text
REJECT
```

message_id 已处理：

```text
IDEMPOTENT
```

---

# 22. ACK

ACK 的语义不是：

> “我收到了”。

而是：

> “我已经把这个 Command 安全持久化到本地，可以在重启后恢复”。

流程：

```text
Control Plane
 ↓
START_JOB
 ↓
Workstation
 ↓
SQLite COMMIT
 ↓
ACK
```

如果 SQLite 没提交成功：

```text
不 ACK
```

---

# 23. Outbox

Workstation 本地使用 Outbox：

```text
Job SUCCESS
 ↓
生成 Event
 ↓
SQLite
 ↓
OUTBOX
 ↓
发送 Control Plane
```

断网：

```text
Control Plane ❌

Event
 ↓
SQLite
```

网络恢复：

```text
Outbox Dispatcher
 ↓
Control Plane
```

保证：

> Workstation 断网期间不会因为无法连接服务器而丢失重要事件。

---

# 24. Resume

网络恢复：

```text
Workstation
 ↓
Connect
 ↓
Resume
 ↓
告诉 Server：
“我最后确认到 sequence N”
```

Server：

```text
N+1
N+2
N+3
...
```

继续同步。

---

# 25. Workstation 本地 SQLite

Workstation 必须使用 SQLite。

主要表：

```text
workstation_state
employees
workspaces
sessions
jobs
job_events
server_commands
worker_events
outbox
provider_installations
updates
```

---

# 26. Control Plane 数据库

Control Plane 推荐 PostgreSQL。

主要表：

```text
users
roles
permissions

employees
employee_skills
employee_knowledge
employee_feishu_bindings

workstations
workstation_certificates

workspaces

jobs
job_events

sessions

messages
conversations

providers
provider_versions
provider_installations

skills
knowledge

approval_requests
approval_policies

secrets
secret_references

artifacts

audit_logs

system_config
```

---

# 27. Admin 权限模型

管理员权限建议 RBAC：

```text
Role
 ↓
Permissions
```

例如：

```text
SUPER_ADMIN
ADMIN
OPERATOR
VIEWER
```

权限：

```text
employee.read
employee.write
employee.delete

workstation.read
workstation.write

job.read
job.cancel

approval.read
approval.approve

secret.read
secret.write

system.read
system.write
```

---

# 28. Approval Center

AI 可以触发需要人类批准的操作。

例如：

```text
Employee
 ↓
Git Push
 ↓
Permission Engine
 ↓
ASK
 ↓
Approval Center
 ↓
User
 ↓
Approve
 ↓
继续
```

---

# 29. Permission 模型

统一三种：

```text
ALLOW
ASK
DENY
```

示例：

| 操作 | 权限 |
|---|---|
| Workspace 读取 | ALLOW |
| Workspace 修改 | ALLOW |
| Git status | ALLOW |
| Git commit | ALLOW |
| Git push | ASK |
| 删除大量文件 | ASK |
| PowerShell | ASK |
| 系统文件修改 | DENY |
| shutdown | DENY |

---

# 30. Approval TOTP

CRITICAL 操作需要 Approval TOTP。

例如：

```text
Git Push
Production Deploy
Delete Workspace
Rotate Credential
```

如果：

```text
Approval TOTP 未配置
```

则：

```text
CRITICAL 操作
       ↓
      DENY
```

不能因为没有 TOTP 而自动放行。

---

# 31. Audit Log

所有重要操作都必须记录：

```text
audit_logs
├── id
├── actor_type
├── actor_id
├── action
├── target_type
├── target_id
├── result
├── ip
├── metadata
└── created_at
```

重点记录：

- Login
- Employee Create
- Employee Update
- Employee Delete
- Workstation Register
- Workstation Revoke
- Job Create
- Job Cancel
- Permission Decision
- Approval
- Secret Access
- Provider Install
- Provider Update
- Workspace Change
- System Config Change

---

# 32. Skill

Skill 是 Employee 能力定义。

例如：

```text
Unity Developer
├── Unity Debugging
├── C# Development
├── UGUI
└── Git
```

Skill 不应该和 Job 混合。

Employee：

```text
Employee
 ↓
Skills
 ↓
Job
```

---

# 33. Knowledge

Knowledge 用于：

- 项目规范
- 编码规范
- 架构文档
- API 文档
- 游戏设计
- 团队规则

关系：

```text
Employee
 ↓
Knowledge
 ↓
Agent Context
```

---

# 34. MCP

MCP 负责 Agent Tool / Context。

例如：

```text
Cursor
 ↓
MCP
 ├── Feishu
 ├── Jira
 ├── Git
 ├── Database
 └── Internal Tools
```

MCP 与 ACP 不要混淆。

```text
ACP
Agent ↔ Runtime / Client

MCP
Agent ↔ Tools / Context
```

---

# 35. ACP

ACP 负责 AI Agent 通信。

核心链路：

```text
Workstation
   │
   ▼
ACP Client
   │
   ▼
Cursor / Codex ACP Server
```

业务代码不要直接操作 Cursor。

错误：

```go
job.go
    cursor.Start()
    cursor.SendMessage()
    cursor.Stop()
```

正确：

```text
JobManager
     ↓
SessionManager
     ↓
AgentSession
     ↓
ACPClient
```

---

# 36. Agent Provider 抽象

不要把 Cursor 写死。

```text
Agent Provider
│
├── Cursor
├── Codex
├── Claude
├── Gemini
└── OpenCode
```

接口：

```go
type AgentProvider interface {
    Name() string

    Detect(ctx context.Context) (*AgentInfo, error)

    Install(ctx context.Context, spec InstallSpec) error

    Uninstall(ctx context.Context) error

    Update(ctx context.Context, spec UpdateSpec) error

    Start(ctx context.Context, spec StartSpec) (AgentSession, error)

    Stop(ctx context.Context, sessionID string) error

    Status(ctx context.Context, sessionID string) (*SessionStatus, error)
}
```

---

# 37. Installer 与 Provider 分离

```text
Cursor
├── CursorInstaller
└── CursorProvider

Codex
├── CodexInstaller
└── CodexProvider
```

Installer：

```go
type ProviderInstaller interface {
    Detect(ctx context.Context) (*InstallInfo, error)
    Install(ctx context.Context, spec InstallSpec) error
    Update(ctx context.Context, spec UpdateSpec) error
    Uninstall(ctx context.Context) error
}
```

Provider：

> 安装以后负责启动、停止、Session 和 ACP。

---

# 38. Provider Registry

不要把安装包直接硬编码进 aew。

Control Plane：

```text
Agent Registry
├── provider
├── version
├── os
├── arch
├── download_url
├── sha256
├── signature
└── release_notes
```

Workstation：

```bash
aew agent cursor install
```

流程：

```text
Server Registry
 ↓
匹配 OS / Arch
 ↓
下载
 ↓
SHA256
 ↓
签名验证
 ↓
安装
```

---

# 39. Workstation Runtime

Workstation 不是普通远程执行器。

定位：

> AI Employee Runtime Host

模块：

```text
Connection Manager
Command Manager
Event Manager
Job Manager
Employee Manager
Session Manager
Provider Manager
Workspace Manager
Security Manager
Monitor
Recovery Manager
Updater
Diagnostic Manager
Artifact Manager
```

---

# 40. Workstation CLI

程序名：

```text
aew
```

命令：

```text
aew
├── version
├── status
├── ping
├── doctor
├── logs
├── register
├── unregister
├── reconnect
│
├── service
│   ├── install
│   ├── uninstall
│   ├── start
│   ├── stop
│   ├── restart
│   └── status
│
├── employee
│   ├── list
│   ├── get
│   └── status
│
├── workspace
│   ├── list
│   ├── get
│   └── check
│
├── agent
│   ├── list
│   ├── providers
│   ├── cursor
│   │   ├── status
│   │   ├── install
│   │   ├── uninstall
│   │   ├── update
│   │   └── version
│   └── codex
│       ├── status
│       ├── install
│       ├── uninstall
│       ├── update
│       └── version
│
├── session
│   ├── list
│   ├── get
│   ├── stop
│   └── kill
│
├── job
│   ├── list
│   └── get
│
├── process
│   ├── list
│   └── inspect
│
├── system
│   ├── info
│   └── requirements
│
├── network
│   ├── status
│   └── test
│
├── cert
│   └── status
│
├── update
│   ├── check
│   └── install
│
└── config
    ├── get
    └── validate
```

---

# 41. CLI 示例

## status

```bash
aew status
```

显示：

```text
Workstation ID : WS-001
Name           : Unity-Main
Status         : ONLINE
Server         : control.example.com
Version        : 0.1.0
PID            : 1234
Uptime         : 4h21m

CPU            : 32%
Memory         : 48%
Disk           : 61%

Employees      : 4
Sessions       : 1

Cursor         : Installed
Codex          : Installed

Heartbeat      : 2s ago
```

---

# 42. ping

不是 ICMP。

流程：

```text
CLI
 ↓
Local IPC
 ↓
Workstation Agent
 ↓
Control Plane
 ↓
Workstation Agent
 ↓
CLI
```

显示：

```text
Control Plane : OK
TLS           : OK
mTLS          : OK
RTT           : 42ms
Sequence      : 18291
Server Time   : ...
```

---

# 43. doctor

```bash
aew doctor
```

检查：

```text
OS
CPU
Memory
Disk
Network
DNS
TLS
mTLS
Control Plane
Certificate
Cursor
Codex
Git
Workspace
ACP
```

可选：

```bash
aew doctor --fix
```

只自动修复安全项目。

---

# 44. Local IPC

CLI 不应该直接读取 SQLite 或自己管理核心进程。

架构：

```text
CLI
 ↓
Local IPC
 ↓
Agent Daemon
 ↓
Runtime Manager
```

Windows：

```text
Named Pipe
```

Linux/macOS：

```text
Unix Domain Socket
```

统一：

```go
type LocalTransport interface {
    Listen() error
    Accept() (net.Conn, error)
}
```

---

# 45. Workstation 目录

## Windows

```text
C:\Program Files\AIEmployee\
    aew.exe
```

配置：

```text
C:\ProgramData\AIEmployee\
    config\
        config.yaml
```

数据：

```text
C:\ProgramData\AIEmployee\
    data\
        workstation.db
        logs\
        cache\
        updates\
```

身份：

```text
C:\ProgramData\AIEmployee\
    identity\
        workstation-id
        client.crt
        client.key
```

Employee：

```text
D:\AIEmployees\
    employees\
        EMP-001\
            workspace\
            runtime\
            cache\
            logs\
            state\
```

---

# 46. Linux

```text
/opt/aie/
    aew
```

配置：

```text
/etc/aie/
    config.yaml
```

数据：

```text
/var/lib/aie/
    workstation.db
    identity/
    cache/
    updates/
```

日志：

```text
/var/log/aie/
```

Employee：

```text
/var/lib/aie/employees/
    EMP-001/
        workspace/
        runtime/
        cache/
        logs/
        state/
```

---

# 47. macOS

```text
/Library/Application Support/AIEmployee/
    config/
    data/
    identity/
    logs/
```

Employee：

```text
/Users/Shared/AIEmployees/
    EMP-001/
```

---

# 48. Platform Paths

不要在业务代码中硬编码路径。

```go
type PlatformPaths interface {
    ProgramDir() string
    ConfigDir() string
    DataDir() string
    IdentityDir() string
    LogDir() string
    WorkspaceRoot() string
}
```

实现：

```text
WindowsPaths
LinuxPaths
DarwinPaths
```

---

# 49. Employee 本地隔离

```text
AIEmployees/
│
├── EMP-001/
│   ├── workspace/
│   ├── runtime/
│   ├── state/
│   ├── cache/
│   └── logs/
│
├── EMP-002/
│   ├── workspace/
│   ├── runtime/
│   ├── state/
│   ├── cache/
│   └── logs/
│
└── EMP-003/
```

核心：

> Employee ID 是稳定本地目录主键。

不能使用 Employee Name 作为主目录。

---

# 50. Workspace

Workspace 是 Employee 实际工作的代码目录。

Workspace 包含：

```text
workspace_id
employee_id
path
repository
branch
permission_policy
```

例如：

```text
EMP-UNITY
 ↓
D:/AIEmployees/EMP-UNITY/workspace
 ↓
Unity Project
```

Workspace Check：

```bash
aew workspace check
```

检查：

- path
- 权限
- Git
- Repository
- Branch
- Disk
- Lock
- 是否被其他 Employee 使用

---

# 51. Session

Session 是实际 AI Agent 运行上下文。

状态：

```text
STARTING
READY
BUSY
IDLE
STOPPING
STOPPED
ERROR
```

V1：

> 一个 Employee 同时只允许一个 Active Session。

以后：

```yaml
max_concurrent_sessions: 2
```

---

# 52. Employee 状态

```text
PROVISIONING
     ↓
STOPPED
     ↓
STARTING
     ↓
IDLE
     ↓
BUSY
     ↓
STOPPING
     ↓
STOPPED
```

异常：

```text
ERROR
```

可增加：

```text
DRAINING
```

---

# 53. Session 持久化

SQLite：

```text
agent_sessions
├── session_id
├── employee_id
├── provider
├── workspace_id
├── process_id
├── status
├── created_at
├── started_at
├── last_activity_at
└── ended_at
```

Workstation 重启：

```text
SQLite
 ↓
发现 RUNNING Session
 ↓
检查 PID
 ↓
进程不存在
 ↓
UNKNOWN
 ↓
Recovery Manager
```

不能直接标记 SUCCESS。

---

# 54. 按需启动 Agent

默认：

```text
Employee = IDLE
```

没有 Cursor 进程。

收到 Job：

```text
Job
 ↓
Employee
 ↓
Start Session
 ↓
Start Cursor
 ↓
ACP
 ↓
READY
 ↓
执行
```

完成：

```text
SUCCESS
 ↓
Session IDLE
```

例如：

```yaml
idle_timeout: 600
```

10 分钟无任务关闭。

---

# 55. Persistent Runtime

支持：

```yaml
runtime:
  mode: persistent
```

适合：

- 核心开发 Employee
- 需要长期上下文
- MCP 长连接
- 长时间工作流

默认仍建议：

```yaml
mode: on-demand
```

---

# 56. Job 执行流程

完整流程：

```text
Feishu Message
       ↓
Message Parser
       ↓
Employee Resolver
       ↓
Permission
       ↓
Job Creator
       ↓
Scheduler
       ↓
Workstation
       ↓
Employee Runtime
       ↓
Session Manager
       ↓
Provider Manager
       ↓
Cursor/Codex
       ↓
ACP
       ↓
Agent
       ↓
Workspace
       ↓
Job Result
       ↓
Artifact
       ↓
Event
       ↓
Control Plane
       ↓
Feishu Reply
```

---

# 57. Recovery Manager

负责：

- Process Crash
- ACP Disconnect
- Workspace unavailable
- Certificate expired
- Disk full
- SQLite issue
- Cursor broken
- Codex missing
- Network unavailable

可能的恢复：

```text
restart session
reconnect ACP
reinstall provider
resume job
report UNKNOWN
```

---

# 58. Resource Scheduler

一台机器可能：

```text
EMP-001 Unity Developer
EMP-002 QA
EMP-003 Build
EMP-004 Research
```

不能无限启动 Agent。

示例：

```yaml
resources:
  cpu: 4
  memory: 8GB
```

Scheduler：

```text
JOB A → RUNNING
JOB B → RUNNING
JOB C → WAITING
```

---

# 59. Security Sandbox

V1：

```text
Employee
    │
    ├── Workspace
    ├── Filesystem Policy
    ├── Command Policy
    ├── Git Policy
    └── Network Policy
```

示例：

```yaml
filesystem:
  allow:
    - D:/Projects/UnityGame/**
    - D:/AIEmployees/EMP-001/**
  deny:
    - C:/Users/**
    - C:/Windows/**
    - D:/Secrets/**
```

---

# 60. Command Policy

禁止直接开放：

```text
shell exec
```

不要把 Workstation 变成：

> 远程 RCE 服务。

只允许业务 Command：

```text
START_JOB
STOP_JOB
START_SESSION
STOP_SESSION
INSTALL_AGENT
UPDATE_AGENT
SYNC_WORKSPACE
```

命令权限：

```text
ALLOW
ASK
DENY
```

---

# 61. V2 / V3 Sandbox

V2/V3：

Windows：

```text
Windows Sandbox
VM
```

Linux：

```text
Docker
Container
VM
```

macOS：

```text
Sandbox
VM
Dedicated User
```

最终：

```text
Employee
 ↓
Sandbox
 ↓
Cursor/Codex
 ↓
Workspace
```

---

# 62. Secret Manager

AI Employee 可能需要：

- Git Token
- NPM Token
- SSH Key
- API Key
- Cloud Credential

绝对不要写进：

```text
Prompt
config.yaml
Job
Message
```

设计：

```text
Employee
 ↓
Secret Reference
 ↓
Secret Manager
 ↓
OS Credential Store / Vault
```

---

# 63. Artifact Manager

Job 可能产生：

```text
代码
Patch
APK
IPA
UnityPackage
日志
截图
测试报告
压缩包
```

设计：

```text
JOB-1001
 ├── result.json
 ├── patch.diff
 └── build.apk
```

Artifact：

```text
artifact_id
job_id
name
type
size
hash
storage
created_at
```

---

# 64. Control Plane ↔ Workstation 协议

建议：

```text
gRPC
```

连接：

```text
Workstation → Control Plane
```

不要要求 Workstation 暴露公网 HTTP Server。

Workstation 主动建立连接：

```text
Workstation
      │
      │ outbound
      ▼
Control Plane
```

这样：

- NAT 友好
- 防火墙简单
- Workstation 不需要公网监听端口

---

# 65. Command 模型

Server → Workstation：

```text
Command
├── command_id
├── workstation_id
├── employee_id
├── job_id
├── type
├── payload
├── sequence
├── timestamp
├── nonce
└── signature / transport authentication
```

Command 类型：

```text
START_JOB
STOP_JOB
START_SESSION
STOP_SESSION
INSTALL_PROVIDER
UPDATE_PROVIDER
SYNC_WORKSPACE
UPDATE_WORKSTATION
```

---

# 66. Event 模型

Workstation → Server：

```text
Event
├── event_id
├── workstation_id
├── employee_id
├── job_id
├── type
├── payload
├── sequence
├── timestamp
└── metadata
```

例如：

```text
SESSION_STARTED
SESSION_READY
SESSION_ERROR

JOB_STARTED
JOB_PROGRESS
JOB_SUCCESS
JOB_FAILED

PROVIDER_INSTALLED
PROVIDER_UPDATED

WORKSPACE_CHANGED
SYSTEM_ALERT
```

---

# 67. 心跳

Workstation 周期发送：

```text
Heartbeat
```

包括：

```text
workstation_id
timestamp
version
cpu
memory
disk
employees
sessions
capabilities
```

例如：

```text
Heartbeat every 5s
```

Server：

```text
> 15s no heartbeat
 ↓
OFFLINE
```

具体时间应做成配置。

---

# 68. Workstation 重启恢复

启动：

```text
Agent Start
 ↓
Load SQLite
 ↓
Recover Sessions
 ↓
Recover Jobs
 ↓
Recover Outbox
 ↓
Connect Server
 ↓
Resume
```

---

# 69. Workstation Service

Windows：

```text
Windows Service
```

Linux：

```text
systemd
```

macOS：

```text
launchd
```

CLI：

```bash
aew service install
aew service start
aew service stop
aew service restart
aew service status
```

---

# 70. Update

Workstation 更新：

```text
Check
 ↓
Download
 ↓
Verify Signature
 ↓
Verify SHA256
 ↓
Install
 ↓
Keep Previous Version
 ↓
Restart
 ↓
Health Check
 ↓
Success
```

失败：

```text
Rollback
```

---

# 71. Docker

Control Plane 部署：

```text
Docker Compose
```

建议初期：

```text
control-plane
postgres
redis (optional)
admin-web
```

如果暂时不需要 Redis，可以先不引入。

核心原则：

> V1 尽量减少基础设施数量。

---

# 72. 推荐 Control Plane 技术栈

Server：

```text
Go
```

API：

```text
gRPC
REST
```

数据库：

```text
PostgreSQL
```

缓存 / Queue：

```text
V1 可不依赖 Redis
V2 根据 Scheduler / Event Bus 需求引入
```

Admin：

```text
React / TypeScript
```

反向代理：

```text
Caddy / Nginx
```

容器：

```text
Docker
Docker Compose
```

---

# 73. 推荐 Workstation 技术栈

```text
Go
SQLite
gRPC
TLS 1.3
mTLS
ACP
```

平台：

```text
Windows
Linux
macOS
```

目标：

> 单文件可执行程序。

---

# 74. Workstation Go Package

```text
cmd/aew
│
└── CLI
      │
      ▼
internal/
│
├── app
├── config
├── identity
│
├── controlplane
│   ├── grpc
│   ├── reconnect
│   ├── heartbeat
│   ├── sequence
│   ├── ack
│   └── resume
│
├── runtime
│   ├── employee
│   ├── session
│   ├── job
│   └── process
│
├── providers
│   ├── interface.go
│   ├── cursor
│   ├── codex
│   └── claude
│
├── acp
├── workspace
├── security
├── monitor
├── updater
├── diagnostics
└── platform
    ├── windows
    ├── linux
    └── darwin
```

---

# 75. Control Plane Go Package

推荐：

```text
cmd/
├── server
└── migrate

internal/
├── api
├── auth
├── user
├── employee
├── workstation
├── workspace
├── job
├── message
├── event
├── scheduler
├── feishu
├── permission
├── approval
├── secret
├── artifact
├── skill
├── knowledge
├── audit
├── registry
├── notification
└── database
```

---

# 76. Admin Frontend

建议：

```text
admin/
├── src/
│   ├── pages/
│   │   ├── dashboard
│   │   ├── employees
│   │   ├── workstations
│   │   ├── jobs
│   │   ├── sessions
│   │   ├── feishu
│   │   ├── skills
│   │   ├── knowledge
│   │   ├── permissions
│   │   ├── approvals
│   │   ├── secrets
│   │   ├── artifacts
│   │   ├── audit
│   │   └── settings
│   │
│   ├── components/
│   ├── api/
│   ├── stores/
│   └── router/
└── package.json
```

---

# 77. Admin 页面与 API

不要让 Admin 页面直接操作数据库。

```text
Admin Web
 ↓
REST API
 ↓
Control Plane
 ↓
Service
 ↓
Repository
 ↓
PostgreSQL
```

实时数据：

```text
Admin Web
 ↓
WebSocket / SSE
 ↓
Control Plane
```

---

# 78. 登录

Admin：

```text
Username / Password
        ↓
Authentication
        ↓
Session / JWT
        ↓
RBAC
```

高风险操作：

```text
Admin Login
 ↓
Approval
 ↓
TOTP
```

密码必须：

- Argon2id / bcrypt
- Rate Limit
- Login Failure Counter
- Audit Log

---

# 79. Login 防暴力破解

必须有：

```text
IP Rate Limit
User Rate Limit
Failed Attempt Counter
Temporary Lock
Audit Log
```

不要永久锁死用户。

---

# 80. 配置系统

推荐：

```text
Default Config
      +
User Config
      ↓
Effective Config
```

不要直接修改 Default Config。

例如：

```text
config/default.yaml
config/production.yaml
```

规则：

```text
production.yaml 存在字段
        ↓
覆盖 default.yaml

不存在
        ↓
使用 default
```

---

# 81. Feishu 配置

Admin：

```text
Feishu
├── App ID
├── App Secret
├── Bot
├── Event Subscription
├── Verification Token
├── Encrypt Key
└── Enabled
```

Secret 必须进入 Secret Manager，而不是明文数据库。

---

# 82. Feishu Employee Binding

一个 Employee：

```text
EMP-UNITY
```

可以绑定：

```text
Feishu Bot Identity
Feishu Open ID
Feishu Chat
```

消息进入后：

```text
Feishu Event
 ↓
Employee Resolver
 ↓
EMP-UNITY
```

---

# 83. Message 去重

Feishu 可能重复投递。

必须保存：

```text
event_id
message_id
```

处理：

```text
if already_processed:
    return success
```

保证：

> Feishu Event 幂等。

---

# 84. Job 幂等

Client 重试：

```text
Create Job
```

不能创建两个 Job。

使用：

```text
idempotency_key
```

例如：

```text
user + message_id
```

---

# 85. Event Bus

初期可以：

```text
PostgreSQL
+
Internal Go Event Bus
```

以后规模增大再考虑：

```text
NATS
Kafka
Redis Streams
```

不要 V1 就为了架构漂亮引入过多中间件。

---

# 86. 日志

分：

```text
Application Log
Audit Log
Job Log
Agent Log
ACP Log
System Log
```

不要把 Audit Log 当普通日志。

Audit Log 必须结构化、长期保存。

---

# 87. Observability

建议：

```text
Metrics
Logs
Tracing
```

指标：

```text
workstation_online
employee_busy
active_sessions
job_running
job_success
job_failed
job_duration
acp_connection
provider_install
cpu
memory
disk
```

以后可接：

```text
Prometheus
Grafana
OpenTelemetry
```

---

# 88. 安全原则

必须遵守：

1. Control Plane 不暴露任意 shell。
2. Workstation 不暴露公网执行端口。
3. Workstation 主动连接 Server。
4. 使用 TLS 1.3。
5. 使用 mTLS。
6. Certificate 可吊销。
7. Command 必须防 Replay。
8. Event 必须幂等。
9. ACK 必须代表本地持久化。
10. CRITICAL 必须 Approval。
11. Approval TOTP 未配置时 CRITICAL 直接拒绝。
12. Secret 不进入 Prompt。
13. Secret 不进入普通日志。
14. Workspace 必须隔离。
15. Admin 使用 RBAC。
16. 高风险操作必须 Audit。

---

# 89. 关键状态恢复原则

任何状态机都必须考虑：

```text
正常执行
 ↓
进程崩溃
 ↓
网络断开
 ↓
机器重启
 ↓
Server 重启
```

不能只设计 Happy Path。

---

# 90. Server 重启

Control Plane 重启后：

```text
PostgreSQL
 ↓
恢复 Employee
恢复 Job
恢复 Session
恢复 Workstation
恢复 Approval
恢复 Message
```

Workstation：

```text
Reconnect
 ↓
Resume
```

---

# 91. Workstation Offline

如果 Workstation Offline：

```text
Scheduler
 ↓
不再分配新的 Job
```

已有 Job：

```text
RUNNING
 ↓
UNKNOWN
```

不要立即：

```text
FAILED
```

因为 Workstation 可能只是网络断开。

---

# 92. Job Timeout

每个 Job：

```text
timeout
```

超时：

```text
RUNNING
 ↓
TIMEOUT
 ↓
STOP SESSION
 ↓
保存 Artifact / Logs
```

---

# 93. Employee DRAINING

当 Workstation 要升级：

```text
ONLINE
 ↓
DRAINING
```

Scheduler：

```text
不再分配新 Job
```

已有 Job：

```text
继续执行
```

全部结束：

```text
OFFLINE
 ↓
UPDATE
```

---

# 94. Workstation Capabilities

Workstation 启动后报告：

```text
OS
ARCH
CPU
MEMORY

providers:
  cursor
  codex

features:
  acp
  sandbox
  docker
```

Scheduler 根据 Capability 选择。

---

# 95. Agent Provider Capability

Provider：

```text
Cursor
```

可能：

```text
ACP = true
MCP = true
headless = true
```

Provider Registry 也应该保存 Capability。

---

# 96. 文件与 Artifact Hash

重要文件：

```text
SHA256
```

用于：

- Provider Package
- Workstation Update
- Artifact
- Patch
- Build

---

# 97. Update Rollback

必须保留上一版本：

```text
version N
version N-1
```

更新：

```text
N
 ↓
N+1
 ↓
Health Check
```

失败：

```text
N+1
 ↓
Rollback
 ↓
N
```

---

# 98. V1 范围

V1 不要一次实现所有能力。

必须完成：

```text
Control Plane
├── Auth
├── Employee
├── Workstation
├── Job
├── Message
├── Feishu
├── Admin
└── PostgreSQL

Workstation
├── CLI
├── Register
├── mTLS
├── gRPC
├── Heartbeat
├── SQLite
├── Employee
├── Workspace
├── Session
├── Job
├── Cursor Provider
├── Codex Provider
└── ACP
```

---

# 99. V2

```text
Permission Engine
Approval Center
Approval TOTP
Secret Manager
Artifact Manager
Resource Scheduler
Provider Registry
Update / Rollback
Advanced Audit
Observability
```

---

# 100. V3

```text
Sandbox
Container
VM
Network Policy
Multi-session
Advanced DAG
Distributed Scheduler
Large-scale Event Bus
```

---

# 101. 第一阶段开发顺序

不要从 Cursor Provider 开始。

推荐：

```text
Phase 1
项目骨架
 ↓
Phase 2
PostgreSQL Schema
 ↓
Phase 3
Control Plane Auth
 ↓
Phase 4
Workstation Identity
 ↓
Phase 5
mTLS + gRPC
 ↓
Phase 6
Heartbeat / ACK / Resume
 ↓
Phase 7
Employee / Workstation CRUD
 ↓
Phase 8
Admin
 ↓
Phase 9
Feishu
 ↓
Phase 10
Job / Scheduler
 ↓
Phase 11
Workstation Runtime
 ↓
Phase 12
ACP
 ↓
Phase 13
Cursor
 ↓
Phase 14
Codex
 ↓
Phase 15
End-to-End
```

---

# 102. Phase 1 — Repository

建议 Monorepo：

```text
ai-employee/
│
├── server/
├── workstation/
├── admin/
├── proto/
├── migrations/
├── deploy/
├── docs/
├── scripts/
└── Makefile
```

---

# 103. Proto

所有 Server ↔ Workstation 协议定义：

```text
proto/
├── common.proto
├── workstation.proto
├── command.proto
├── event.proto
├── heartbeat.proto
├── job.proto
└── session.proto
```

不要在 Go 代码中手写通信结构。

Proto 是协议唯一来源。

---

# 104. Database Migration

必须使用 Migration：

```text
migrations/
├── 000001_init.sql
├── 000002_users.sql
├── 000003_employees.sql
├── 000004_workstations.sql
├── 000005_jobs.sql
└── ...
```

禁止启动时偷偷修改生产数据库。

---

# 105. API 设计

Admin REST：

```text
POST   /api/auth/login

GET    /api/employees
POST   /api/employees
GET    /api/employees/:id
PATCH  /api/employees/:id
DELETE /api/employees/:id

GET    /api/workstations
GET    /api/workstations/:id

GET    /api/jobs
GET    /api/jobs/:id
POST   /api/jobs
POST   /api/jobs/:id/cancel

GET    /api/sessions
GET    /api/approvals
POST   /api/approvals/:id/approve

GET    /api/audit-logs
```

实际路径可根据最终 API Convention 调整。

---

# 106. WebSocket / SSE

Admin 需要实时：

```text
Workstation Online
Workstation Offline
Job Progress
Session Status
Approval Request
System Alert
```

建议：

```text
SSE
```

作为 V1 简单实时方案。

需要双向通信时再使用 WebSocket。

---

# 107. Feishu Webhook

```text
POST /api/integrations/feishu/events
```

流程：

```text
Verify Signature
 ↓
Parse Event
 ↓
Deduplicate
 ↓
Resolve Employee
 ↓
Create Message
 ↓
Create Job
 ↓
Return 200
```

不要在 Webhook HTTP 请求中同步等待 Cursor 完成。

---

# 108. Async Job

Feishu：

```text
HTTP Request
 ↓
Create Message
 ↓
Create Job
 ↓
Return 200
```

之后：

```text
Scheduler
 ↓
Workstation
```

完成：

```text
Job SUCCESS
 ↓
Notification
 ↓
Feishu Reply
```

---

# 109. Notification

统一：

```text
Notification Service
```

可以发送：

```text
Feishu
Admin
Email
```

以后扩展：

```text
Telegram
Slack
```

---

# 110. Job DAG / 协作

V1 可以先：

```text
Job A
 ↓
Job B
```

以后支持：

```text
             ┌── QA
Developer ───┤
             └── Build
                  │
                  ▼
                Deploy
```

形成 DAG。

---

# 111. Employee Collaboration

以后可以：

```text
Employee A
 ↓
Create Job for Employee B
 ↓
Employee B
 ↓
Artifact
 ↓
Employee A
```

不要让 Employee A 直接访问 Employee B 的本地进程。

所有协作经过 Control Plane：

```text
Employee
 ↓
Message / Job
 ↓
Control Plane
 ↓
Scheduler
 ↓
Employee
```

---

# 112. Job Context

Job 应保存：

```text
job_id
employee_id
workspace_id
session_id
prompt
attachments
parent_job_id
created_by
created_at
started_at
completed_at
status
result
```

---

# 113. Job Event

完整历史：

```text
JOB_CREATED
JOB_ASSIGNED
JOB_STARTED
SESSION_STARTED
SESSION_READY
AGENT_MESSAGE
TOOL_CALL
JOB_PROGRESS
APPROVAL_REQUIRED
APPROVAL_GRANTED
JOB_SUCCESS
JOB_FAILED
JOB_CANCELLED
```

这样 Admin 可以看到完整 Timeline。

---

# 114. Admin Job Timeline

页面：

```text
JOB-1001

09:10 Created
09:10 Assigned EMP-UNITY
09:10 Workstation WS-001
09:11 Session Started
09:11 ACP Ready
09:12 Agent Started
09:15 Files Changed
09:18 Git Commit
09:19 Job Success
```

---

# 115. Workstation Monitor

Monitor：

```text
CPU
Memory
Disk
Network
Processes
Agent Sessions
```

不要高频把所有指标写数据库。

可以：

```text
Heartbeat
 ↓
Current State
```

长期监控：

```text
Metrics System
```

---

# 116. Process Manager

只允许业务层调用：

```text
Start Process
Stop Process
Inspect Process
```

不要暴露：

```text
Execute Arbitrary Command
```

例如：

```go
type ProcessManager interface {
    Start(spec ProcessSpec) (*Process, error)
    Stop(pid int) error
    Inspect(pid int) (*ProcessInfo, error)
}
```

---

# 117. ACP Session Interface

```go
type AgentSession interface {
    Start(ctx context.Context) error

    Send(ctx context.Context, input AgentInput) error

    Events() <-chan AgentEvent

    Stop(ctx context.Context) error
}
```

Job Manager 只依赖：

```text
AgentSession
```

不依赖：

```text
Cursor
Codex
```

---

# 118. Provider 启动流程

```text
SessionManager
 ↓
ProviderManager
 ↓
CursorProvider
 ↓
CursorInstaller / Detect
 ↓
Start Cursor
 ↓
Wait ACP
 ↓
ACP Handshake
 ↓
Session READY
```

---

# 119. Provider 生命周期

```text
NOT_INSTALLED
 ↓
INSTALLING
 ↓
INSTALLED
 ↓
UPDATING
 ↓
INSTALLED
```

异常：

```text
ERROR
```

---

# 120. Workspace Lock

避免：

```text
Employee A
Employee B
```

同时修改同一 Workspace。

V1：

```text
Workspace Lock
```

以后：

```text
Git Worktree
```

或：

```text
Per Employee Workspace
```

---

# 121. Unity 项目特殊处理

由于主要场景可能是 Unity 开发，Workspace Check 后续可增加：

```text
Unity Version
Unity Editor Path
Library
Packages
ProjectSettings
Git
```

Provider 可以支持：

```text
Unity Editor
Unity CLI
Build Pipeline
Test Runner
```

这些属于 Unity-specific Capability，不应该污染通用 Runtime。

---

# 122. Workstation CLI 与 Agent Daemon

CLI：

```text
aew status
```

内部：

```text
Local IPC
 ↓
Daemon
```

Daemon 才是真正的：

```text
Workstation Runtime
```

这样即使 CLI 退出，Employee 和 Job 仍然运行。

---

# 123. Daemon 启动

```text
OS
 ↓
Service Manager
 ↓
aew daemon
 ↓
Load Config
 ↓
Load Identity
 ↓
Open SQLite
 ↓
Recover State
 ↓
Connect Control Plane
 ↓
Heartbeat
 ↓
Ready
```

---

# 124. 配置示例

```yaml
workstation:
  id: ""
  name: "Unity-Main"

control_plane:
  endpoint: "control.example.com:443"

runtime:
  max_sessions: 2
  idle_timeout: 600

providers:
  cursor:
    enabled: true
  codex:
    enabled: true

security:
  require_mtls: true

workspace:
  root: "D:/AIEmployees"
```

敏感信息不放 YAML。

---

# 125. Workstation Identity

首次运行：

```text
aew
 ↓
Generate Identity
 ↓
Register
```

之后：

```text
workstation-id
private key
certificate
```

私钥：

> 永远不能上传 Control Plane。

---

# 126. Certificate 生命周期

```text
ENROLL
 ↓
ACTIVE
 ↓
RENEW
 ↓
EXPIRED
```

Server 可以：

```text
REVOKE
```

Workstation：

```text
cert status
```

---

# 127. Reconnect

网络断开：

```text
CONNECTED
 ↓
DISCONNECTED
 ↓
RECONNECTING
 ↓
CONNECTED
```

采用指数退避：

```text
1s
2s
4s
8s
...
```

设置最大值。

---

# 128. Clock / Time

涉及防 Replay 时：

不要只依赖客户端本地时间。

使用：

```text
Server Timestamp
```

并允许合理 clock skew。

重要状态同步以 Server 时间为准。

---

# 129. Idempotency

所有关键操作必须可重试。

例如：

```text
START_JOB
INSTALL_PROVIDER
UPDATE_WORKSTATION
```

重复发送：

```text
Command ID 相同
 ↓
不重复执行
```

---

# 130. 禁止的架构

不要：

```text
Control Plane
 ↓
SSH
 ↓
Workstation
 ↓
Shell
```

也不要：

```text
Admin
 ↓
直接数据库
```

也不要：

```text
Job
 ↓
直接 Cursor
```

正确：

```text
Admin
 ↓
API
 ↓
Control Plane
 ↓
Scheduler
 ↓
Workstation
 ↓
Runtime
 ↓
Provider
 ↓
ACP
```

---

# 131. V1 最小闭环

必须先跑通：

```text
Admin 创建 Employee
        ↓
Employee 绑定 Workstation
        ↓
Employee 绑定 Workspace
        ↓
Workstation Online
        ↓
Feishu @ Employee
        ↓
Control Plane 创建 Job
        ↓
Scheduler 分配 Job
        ↓
Workstation 创建 Session
        ↓
启动 Cursor/Codex
        ↓
ACP
        ↓
执行任务
        ↓
Job Success
        ↓
Control Plane
        ↓
Feishu 回复
```

这是整个系统的第一条 Golden Path。

---

# 132. Golden Path 验收标准

## Server

- PostgreSQL 正常
- Admin 可以登录
- 创建 Employee
- 创建 Workstation
- 绑定 Workspace
- 查看状态

## Workstation

```bash
aew register
aew status
aew ping
aew doctor
```

全部正常。

## Security

- mTLS 成功
- 非合法 Certificate 无法连接
- Replay Command 被拒绝
- Duplicate Command 幂等

## Feishu

```text
@Employee
```

成功创建 Job。

## Runtime

```text
Job
 ↓
Session
 ↓
Cursor
 ↓
ACP
```

成功。

---

# 133. Codex 开发原则

Codex 开发时必须：

1. 不跳过协议设计。
2. 不把 Cursor 写死在 Job。
3. 不让 Server 执行任意 Shell。
4. 不让 Admin 直接操作 DB。
5. 所有状态机明确枚举。
6. 所有关键操作支持幂等。
7. 所有 Server ↔ Workstation 消息支持 sequence。
8. 所有重要状态支持恢复。
9. 所有敏感操作进入 Audit Log。
10. Secret 不进入日志。
11. Provider 使用接口隔离。
12. ACP 单独抽象。
13. 平台相关代码进入 platform。
14. V1 不过度引入微服务。
15. 优先完成 Golden Path。

---

# 134. 推荐 Monorepo 最终结构

```text
ai-employee/
│
├── server/
│   ├── cmd/
│   ├── internal/
│   ├── migrations/
│   └── tests/
│
├── workstation/
│   ├── cmd/
│   ├── internal/
│   └── tests/
│
├── admin/
│   ├── src/
│   └── tests/
│
├── proto/
│
├── deploy/
│   ├── docker-compose.yml
│   ├── Dockerfile.server
│   └── Dockerfile.admin
│
├── docs/
│
├── scripts/
│
├── Makefile
└── README.md
```

---

# 135. 开发任务拆分

## Epic 1 — Foundation

- Monorepo
- Go Modules
- Admin project
- Proto
- Docker
- PostgreSQL
- Migration
- CI

## Epic 2 — Identity

- User
- Workstation Identity
- Enrollment
- Certificate
- mTLS

## Epic 3 — Control Plane

- Employee
- Workstation
- Workspace
- Job
- Session
- Message

## Epic 4 — Admin

- Login
- Dashboard
- Employee
- Workstation
- Job
- Session
- Audit

## Epic 5 — Feishu

- Bot
- Event
- Signature
- Deduplication
- Employee Resolver
- Reply

## Epic 6 — Workstation

- Daemon
- CLI
- SQLite
- Local IPC
- Runtime
- Process Manager

## Epic 7 — Agent

- Provider interface
- Installer
- Cursor
- Codex
- ACP

## Epic 8 — Reliability

- ACK
- Sequence
- Outbox
- Resume
- Recovery
- Reconnect

## Epic 9 — Security

- RBAC
- Permission
- Approval
- TOTP
- Secret
- Audit

## Epic 10 — Operations

- Update
- Rollback
- Diagnostics
- Metrics
- Artifact

---

# 136. 测试策略

必须包含：

```text
Unit Test
Integration Test
Protocol Test
Security Test
E2E Test
Recovery Test
```

重点测试：

```text
Network Disconnect
Server Restart
Workstation Restart
Cursor Crash
ACP Disconnect
Duplicate Command
Out-of-order Event
Certificate Expired
Certificate Revoked
Disk Full
Workspace Missing
Provider Missing
Job Timeout
```

---

# 137. 最终系统结构

```text
                                      ┌──────────────┐
                                      │     User     │
                                      └──────┬───────┘
                                             │
                                           Feishu
                                             │
                                             ▼
┌─────────────────────────────────────────────────────────────────────┐
│                         CONTROL PLANE                              │
│                           Docker                                   │
│                                                                     │
│  Admin API ─── Admin Web                                            │
│       │                                                             │
│       ├── Auth / RBAC                                               │
│       ├── Employee Manager                                          │
│       ├── Workstation Manager                                      │
│       ├── Workspace Manager                                        │
│       ├── Message Bus                                               │
│       ├── Job Manager                                               │
│       ├── Scheduler                                                 │
│       ├── Feishu Integration                                        │
│       ├── Permission Engine                                         │
│       ├── Approval Center                                           │
│       ├── Secret Manager                                            │
│       ├── Artifact Manager                                          │
│       ├── Skill / Knowledge                                         │
│       └── Audit Log                                                 │
│                                                                     │
│                         PostgreSQL                                 │
└──────────────────────────────┬──────────────────────────────────────┘
                               │
                         gRPC / TLS 1.3
                               │
                              mTLS
                               │
                ┌──────────────┴──────────────┐
                │                             │
                ▼                             ▼
       ┌───────────────────┐        ┌───────────────────┐
       │   Workstation A   │        │   Workstation B   │
       │                   │        │                   │
       │ aew Daemon        │        │ aew Daemon        │
       │ SQLite            │        │ SQLite            │
       │ Runtime Manager   │        │ Runtime Manager   │
       │ Session Manager   │        │ Session Manager   │
       │ Job Manager       │        │ Job Manager       │
       │ Provider Manager  │        │ Provider Manager  │
       │ Workspace Manager │        │ Workspace Manager │
       │ Security Manager  │        │ Security Manager  │
       │                   │        │                   │
       │ EMP-001           │        │ EMP-003           │
       │ EMP-002           │        │ EMP-004           │
       │                   │        │                   │
       │ Cursor            │        │ Codex             │
       │   │               │        │   │               │
       │  ACP              │        │  ACP              │
       │   │               │        │   │               │
       │ Workspace         │        │ Workspace         │
       └───────────────────┘        └───────────────────┘
```

---

# 138. 最终设计原则

整个项目长期必须坚持四个边界：

```text
┌─────────────────────────────────────────┐
│ Control Plane                           │
│                                         │
│ What / Who / When                       │
│ 业务、身份、调度、管理                   │
└───────────────────┬─────────────────────┘
                    │
                    │ Secure Protocol
                    ▼
┌─────────────────────────────────────────┐
│ Workstation                             │
│                                         │
│ Where / How                             │
│ Runtime、Process、Workspace             │
└───────────────────┬─────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ ACP                                     │
│                                         │
│ AI Agent Communication                  │
└───────────────────┬─────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ MCP                                     │
│                                         │
│ Tools / Context                         │
└───────────────────┬─────────────────────┘
                    │
                    ▼
┌─────────────────────────────────────────┐
│ Workspace                               │
│                                         │
│ Code / Files / Project                  │
└─────────────────────────────────────────┘
```

最终目标不是：

> “做一个能远程运行 Cursor 的程序”。

而是：

> **构建一个可以长期运行、管理多个 AI Employee、连接多个 Workstation、使用不同 AI Agent、通过 Feishu 协作，并具备安全、审批、恢复、审计和扩展能力的 AI Employee Runtime Platform。**

Codex 应以本文档作为总体设计基线，在具体实现中优先完成 V1 Golden Path，再逐步扩展 V2/V3 能力。
