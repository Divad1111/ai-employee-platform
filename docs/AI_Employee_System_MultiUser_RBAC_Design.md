# AI Employee System 多用户、权限与资源隔离设计

> 版本：v2.0  
> 文档用途：交由 Codex 直接进行系统设计与开发  
> 适用范围：Center Server、Admin-Web、Workstation、Digital Employee、Token/Quota、Audit

---

# 1. 目标

当前系统从“单用户 AI 员工管理平台”升级为“全公司多用户 AI Employee Platform”。

系统必须支持：

1. 用户注册、启用、禁用。
2. 用户角色与权限管理。
3. 页面级权限控制。
4. API/功能级权限控制。
5. 数据范围（Scope）控制。
6. Workstation 归属于 User。
7. Digital Employee 归属于 User。
8. Digital Employee 同时绑定一个 Workstation。
9. 用户、Workstation、Digital Employee 的 Token / Request 配额。
10. Admin 可以查看和管理全公司数据。
11. 普通用户只能看到自己拥有或授权范围内的数据。
12. Audit 页面必须根据数据权限自动过滤。
13. 前端隐藏不是安全边界，后端 API 必须强制执行权限和数据 Scope。
14. 保持现有 Center Server + Admin-Web + Workstation 架构。
15. 为未来部门、团队、项目级权限预留扩展能力。

---

# 2. 总体架构

```text
                           ┌───────────────────────┐
                           │       Admin-Web       │
                           │  用户 / 工作站 / 员工   │
                           │  Token / Audit / 配置  │
                           └───────────┬───────────┘
                                       │ HTTPS
                                       ▼
┌──────────────────────────────────────────────────────────────┐
│                         Center Server                         │
│                                                              │
│  Authentication                                             │
│  Authorization / RBAC / Scope                               │
│  User Management                                            │
│  Workstation Management                                     │
│  Digital Employee Management                                │
│  Token / Quota Management                                   │
│  Task / Automation                                          │
│  Audit                                                       │
│  Webhook / Scheduler / Calendar                              │
│                                                              │
└───────────────┬───────────────────────┬──────────────────────┘
                │                       │
          Secure Connection        Secure Connection
                │                       │
                ▼                       ▼
      ┌──────────────────┐    ┌──────────────────┐
      │   Workstation A  │    │   Workstation B  │
      │                  │    │                  │
      │ Employee 1       │    │ Employee 3       │
      │ Employee 2       │    │ Employee 4       │
      │                  │    │                  │
      │ Cursor / Codex   │    │ Cursor / Codex   │
      └──────────────────┘    └──────────────────┘
```

---

# 3. 核心设计原则

## 3.1 User 是资源归属的第一层

```text
User
 ├── Workstation
 │     ├── Digital Employee
 │     └── Token / Quota
 │
 └── Digital Employee
       ├── Workstation
       └── Token / Quota
```

数据库必须明确：

```text
workstations.owner_user_id
digital_employees.owner_user_id
digital_employees.workstation_id
```

---

## 3.2 不使用代码中的 isAdmin 判断作为权限系统

禁止大量出现：

```go
if user.IsAdmin {
    ...
}
```

正确方式：

```text
User
 ↓
Role
 ↓
Permission
 ↓
Scope
 ↓
Resource
```

管理员只是拥有更大 Scope 的角色。

---

## 3.3 页面权限、功能权限、数据权限必须分离

### 页面权限

控制：

```text
用户是否可以看到 Admin-Web 某个菜单 / Tab
```

### 功能权限

控制：

```text
用户可以做什么
```

例如：

```text
workstation.read
workstation.create
workstation.update
workstation.delete
workstation.execute
```

### 数据 Scope

控制：

```text
用户可以操作谁的数据
```

例如：

```text
own
all
department
team
project
```

---

# 4. 用户模型

## 4.1 User

建议字段：

```text
id
username
display_name
email
avatar_url
status
role_id
timezone
locale
created_at
updated_at
last_login_at
```

status：

```text
active
disabled
pending
```

---

# 5. Role

初始系统角色：

```text
SYSTEM_ADMIN
USER
OPERATOR
AUDITOR
```

角色不是硬编码逻辑，而是数据库实体。

未来可以增加：

```text
TOKEN_ADMIN
WORKSTATION_ADMIN
EMPLOYEE_ADMIN
```

---

# 6. Permission

Permission 使用稳定字符串。

例如：

```text
user.read
user.create
user.update
user.disable
user.delete

role.read
role.create
role.update
role.delete

workstation.read
workstation.create
workstation.update
workstation.delete
workstation.execute

digital_employee.read
digital_employee.create
digital_employee.update
digital_employee.delete
digital_employee.execute

quota.read
quota.update

token.read
token.update

audit.read
audit.export

task.read
task.create
task.update
task.delete
task.execute

automation.read
automation.create
automation.update
automation.delete
automation.execute

system.read
system.update
```

---

# 7. Permission Scope

Permission 不只判断是否允许，还必须判断 Scope。

建议：

```text
ALL
OWN
ASSIGNED
NONE
```

未来扩展：

```text
DEPARTMENT
TEAM
PROJECT
```

例如：

```text
workstation.read = OWN
audit.read = OWN
user.read = NONE
```

管理员：

```text
workstation.read = ALL
audit.read = ALL
user.read = ALL
```

---

# 8. RBAC 数据模型

推荐：

```text
users
roles
permissions
role_permissions
```

关系：

```text
User
  │
  ▼
Role
  │
  ▼
RolePermission
  │
  ▼
Permission + Scope
```

role_permissions 建议：

```text
role_id
permission_id
scope
created_at
```

---

# 9. Workstation

# 9.1 Workstation 多用户共享模型

Workstation 与 User 必须采用：

```text
User N : M Workstation
```

而不是：

```text
User 1 : N Workstation
```

因此必须新增：

```text
workstation_users
```

关系：

```text
User A ─────┐
User B ─────┼── Workstation X
User C ─────┘
```

同一个 Workstation 可以同时承载多个用户的 Digital Employee。

示例：

```text
Workstation X
│
├── User A
│    ├── Employee A1
│    └── Employee A2
│
├── User B
│    └── Employee B1
│
└── User C
     └── Employee C1
```

访问 Workstation 时必须同时检查：

```text
用户身份
+
workstation_users 授权关系
+
Permission
+
Scope
```

---


Workstation 必须属于 User。

```text
workstations
```

建议字段：

```text
id
owner_user_id
name
machine_id
platform
arch
version
status
last_seen_at
registered_at
created_at
updated_at
```

关键关系：

```text
workstations.owner_user_id -> users.id
```

---

# 10. Digital Employee

Digital Employee 必须：

1. 归属于一个 User。
2. 绑定一个 Workstation。
3. 在该 Workstation 上拥有独立的工作目录。
4. 不要求 Workstation 与 Digital Employee 的 User 一一对应。

建议字段：

```text
id
owner_user_id
workstation_id
name
identifier
description
status
agent_type
working_directory
session_id
created_at
updated_at
```

关系：

```text
digital_employees.owner_user_id -> users.id

digital_employees.workstation_id -> workstations.id
```

注意：

**Workstation 可以被多个 User 共享。**

因此禁止再使用：

```text
digital_employee.owner_user_id == workstation.owner_user_id
```

作为绑定条件。

正确模型是：

```text
Workstation A
│
├── User A
│    ├── Employee A1
│    └── Employee A2
│
├── User B
│    └── Employee B1
│
└── User C
     └── Employee C1
```

Digital Employee 的 User 归属由：

```text
digital_employees.owner_user_id
```

决定。

Workstation 的访问权限则由独立的：

```text
workstation_users
```

关系决定。


---

# 11. User 与资源关系

Workstation 与 User 是 **多对多关系**。

一个 Workstation 可以被多个 User 使用，一个 User 也可以使用多个 Workstation。

因此最终结构：

```text
User A
│
├── Workstation A
│    ├── Employee A1
│    └── Employee A2
│
└── Workstation B
     └── Employee A3


User B
│
├── Workstation A
│    └── Employee B1
│
└── Workstation C
     └── Employee B2
```

其中：

```text
Workstation A
├── User A
│    ├── Employee A1
│    └── Employee A2
│
└── User B
     └── Employee B1
```

管理员：

```text
System Admin
└── 可以查看和管理全部 User / Workstation / Employee
```

## 11.1 Workstation User Access

必须增加中间表：

```text
workstation_users
```

建议字段：

```text
id
workstation_id
user_id
role
status
created_at
updated_at
```

其中 role 第一版可以：

```text
OWNER
MEMBER
```

或者更简单：

```text
OWNER
USER
```

但从长期扩展考虑，推荐使用独立权限 Scope，而不是大量依赖 Workstation Role。

Workstation 的访问关系：

```text
Workstation A
     │
     ├── User A
     ├── User B
     └── User C
```

只有存在：

```text
workstation_users(workstation_id, user_id)
```

的用户，才可以访问该 Workstation。

管理员通过 `ALL` Scope 可以访问全部 Workstation。

---

# 11.2 Digital Employee 与 Workstation

Digital Employee 是：

```text
User 1 : N Digital Employee
Workstation 1 : N Digital Employee
```

但一个 Digital Employee 只有一个 owner User 和一个运行 Workstation：

```text
Digital Employee
├── owner_user_id = User A
└── workstation_id = Workstation X
```

创建 Employee 时必须同时验证：

```text
User A
   ↓
是否拥有/被授权访问
   ↓
Workstation X
```

只有授权成功才能创建。

因此：

```text
User A
 └── Employee A
      └── Workstation X
```

是合法的。

而：

```text
User A
 └── Employee A
      └── Workstation Y
```

如果 User A 没有 Workstation Y 的访问权限，则必须拒绝。

---

# 11.3 Workstation 不属于单个 User

旧设计中的：

```text
workstations.owner_user_id
```

不再作为 Workstation 的唯一归属字段。

推荐改为：

```text
workstations
```

只描述 Workstation 本身：

```text
id
name
machine_id
platform
arch
version
status
last_seen_at
registered_at
created_at
updated_at
```

User 与 Workstation 的关系由：

```text
workstation_users
```

维护。

如果业务上需要记录“最初创建者”，可以额外保留：

```text
created_by_user_id
```

但它不是访问权限依据。


---

# 12. Token / Quota 设计

由于一个 Workstation 可以被多个 User 共享，Quota 必须严格区分：

```text
User Quota
Workstation Quota
Digital Employee Quota
```

Workstation Quota 是机器/运行环境层面的总资源限制。

User Quota 是用户自己的 Token / Request 使用限制。

Digital Employee Quota 是具体数字员工的限制。

建议不要简单理解成：

```text
User quota → Workstation quota → Employee quota
```

因为多个用户可以共享同一个 Workstation。

正确模型是：

```text
                 Workstation
                 100M token
                 /         \
                /           \
           User A           User B
            60M              40M
             │                │
        ┌────┴────┐       ┌───┴───┐
        ▼         ▼       ▼       ▼
       E-A1      E-A2    E-B1    E-B2
       20M       40M     10M     30M
```

其中：

```text
Workstation quota
```

控制这台机器整体允许消耗多少资源。

而：

```text
User quota
```

控制用户自身的资源使用上限。

因此多个 User 的分配总量不能超过 Workstation 的可用配额（如果系统启用 Workstation 配额分配机制）。


Token 配额必须独立建模，不直接硬编码在 User 表。

建议实体：

```text
quota_policies
quota_usage
token_accounts
token_usage
```

---

# 13. Quota Policy

Quota 可以分别配置：

```text
User
Workstation
Digital Employee
```

建议字段：

```text
id
resource_type
resource_id
period_type
token_limit
request_limit
concurrency_limit
enabled
created_at
updated_at
```

resource_type：

```text
USER
WORKSTATION
DIGITAL_EMPLOYEE
```

period_type：

```text
DAILY
WEEKLY
MONTHLY
YEARLY
```

由于 Workstation 可以被多个 User 共享，还建议增加：

```text
workstation_user_quotas
```

用于定义某个 User 在某个 Workstation 上的独立配额。

字段：

```text
id
workstation_id
user_id
period_type
token_limit
request_limit
concurrency_limit
enabled
created_at
updated_at
```

最终可以形成：

```text
Workstation A
├── Workstation Quota: 100M
│
├── User A on A: 60M
│   ├── Employee A1: 20M
│   └── Employee A2: 40M
│
└── User B on A: 40M
    └── Employee B1: 40M
```

这样可以避免“User A 在 Workstation A 上的配额”和“User A 在 Workstation B 上的配额”混在一起。


Quota 可以分别配置：

```text
User
Workstation
Digital Employee
```

例如：

```text
User:
100M token / month

Workstation A:
60M / month

Employee UI:
20M / month

Employee Code:
30M / month
```

建议字段：

```text
id
resource_type
resource_id
period_type
token_limit
request_limit
concurrency_limit
enabled
created_at
updated_at
```

resource_type：

```text
USER
WORKSTATION
DIGITAL_EMPLOYEE
```

period_type：

```text
DAILY
WEEKLY
MONTHLY
YEARLY
```

---

# 14. Quota 继承与限制

建议采用“上级总量 + 下级分配”的限制模型。

例如：

```text
User
100M
│
├── WS-A
│   60M
│   ├── Employee-1 20M
│   └── Employee-2 40M
│
└── WS-B
    40M
```

必须满足：

```text
Workstation quota <= User quota
```

以及：

```text
sum(Employee quota)
<= Workstation quota
```

如果未配置下级独立 quota：

```text
继承上级 quota policy
```

---

# 15. Token 账户

如果系统支持多个 Token / API Key：

```text
token_accounts
```

建议字段：

```text
id
owner_user_id
name
provider
token_type
secret_ref
status
created_at
updated_at
last_used_at
```

重要：

**数据库中禁止明文保存第三方 API Token。**

应使用：

```text
secret_ref
```

关联安全存储。

如果第一期没有独立 Secret Manager，可以：

```text
AES-256-GCM
+
Center Server 主密钥
```

并为未来 Vault/KMS 留接口。

---

# 16. Audit Log

Audit 是整个权限系统的重要组成部分。

建议字段：

```text
id
actor_user_id
action
resource_type
resource_id
target_user_id
workstation_id
digital_employee_id
request_id
ip
user_agent
result
metadata
created_at
```

例如：

```text
actor_user_id = User A
action = digital_employee.update
resource_id = Employee-001
result = success
```

---

# 17. Audit 数据 Scope

管理员：

```text
audit.read = ALL
```

普通用户：

```text
audit.read = OWN
```

OWN 必须定义为：

```text
actor_user_id = current_user_id
OR
resource owner = current_user_id
```

建议第一版默认：

```text
用户可以看到：
1. 自己执行的操作
2. 自己资源产生的操作
```

管理员可以看到全部。

---

# 18. 后端权限检查

所有 API 必须经过：

```text
Authentication
      ↓
Authorization
      ↓
Permission Check
      ↓
Scope Resolution
      ↓
Resource Query Filter
      ↓
Business Logic
```

示例：

```http
GET /api/workstations
```

后台：

```text
1. 获取 current_user
2. 检查 workstation.read
3. 解析 Scope
4. Scope=ALL：
       查询全部
5. Scope=OWN：
       WHERE owner_user_id=current_user.id
6. 返回数据
```

---

# 19. 禁止前端自行决定数据权限

错误：

```javascript
if (isAdmin) {
    loadAllUsers()
}
```

正确：

```text
Frontend
    ↓
GET /api/users
    ↓
Backend Authorization
    ↓
根据权限决定返回范围
```

即使普通用户手工调用：

```text
/api/users
/api/workstations
/api/audit-logs
```

也不能获取未授权数据。

---

# 20. Admin-Web 页面结构

最终：

```text
Admin-Web
│
├── Dashboard
│
├── 用户管理
│   ├── 用户列表
│   ├── 用户详情
│   └── 角色与权限
│
├── 工作站管理
│
├── 数字员工管理
│
├── 任务调度流转
│   ├── 任务
│   └── 自动化任务
│
├── Token / 配额管理
│
├── 审计日志
│
├── 系统监控
│
└── 系统配置
```

---

# 21. 用户管理

## 21.1 用户列表

显示：

```text
用户名
姓名
邮箱
角色
状态
工作站数量
数字员工数量
Token 使用量
最后登录时间
创建时间
```

操作：

```text
查看
编辑
禁用
启用
删除
重置认证信息
```

删除需要谨慎。

推荐默认：

```text
禁用用户
```

而不是物理删除。

---

# 22. 用户详情

页面建议：

```text
用户详情
│
├── 基本信息
├── 权限
├── 工作站
├── 数字员工
├── Token / 配额
└── 审计
```

示意：

```text
┌───────────────────────────────────────────┐
│ 张三                         [禁用用户]     │
├───────────────────────────────────────────┤
│ 基本信息                                   │
│ Email: xxx@company.com                    │
│ Role: USER                                │
│ Status: ACTIVE                             │
├───────────────────────────────────────────┤
│ Workstations                              │
│ WS-MAC-001       Online       3 Employees │
│ WS-WIN-002       Online       5 Employees │
├───────────────────────────────────────────┤
│ Digital Employees                         │
│ UI Employee                               │
│ Code Employee                             │
│ Build Employee                            │
├───────────────────────────────────────────┤
│ Quota                                     │
│ Monthly Token: 100M                       │
│ Used: 37M                                 │
└───────────────────────────────────────────┘
```

---

# 23. 角色与权限页面

Admin：

```text
用户管理
└── 角色与权限
```

角色列表：

```text
SYSTEM_ADMIN
USER
OPERATOR
AUDITOR
```

权限矩阵：

```text
Permission                  USER   OPERATOR   AUDITOR   ADMIN
----------------------------------------------------------------
user.read                    -        -          -        ALL
workstation.read            OWN      ALL        -        ALL
workstation.create          OWN      ALL        -        ALL
workstation.update          OWN      ALL        -        ALL
digital_employee.read       OWN      ALL        -        ALL
digital_employee.execute    OWN      ALL        -        ALL
audit.read                  OWN      ALL        ALL      ALL
quota.read                  OWN      ALL        -        ALL
quota.update                -        ALL        -        ALL
```

注意：

这里的 `ALL` / `OWN` 是 Scope，不是权限名称。

---

# 24. 页面可见性

前端根据后端返回的权限集合决定菜单是否显示。

建议登录后获取：

```http
GET /api/me
```

返回：

```json
{
  "user": {
    "id": "...",
    "name": "...",
    "role": "USER"
  },
  "permissions": [
    {
      "name": "workstation.read",
      "scope": "OWN"
    },
    {
      "name": "digital_employee.read",
      "scope": "OWN"
    },
    {
      "name": "audit.read",
      "scope": "OWN"
    }
  ]
}
```

前端根据 Permission 渲染菜单。

但是：

**前端菜单隐藏只是 UX，不是安全机制。**

---

# 25. 页面级数据隔离

例如：

## Workstation 页面

普通用户：

```text
只显示 owner_user_id = current_user.id
```

管理员：

```text
显示全部
```

---

## Digital Employee 页面

普通用户：

```text
owner_user_id = current_user.id
```

管理员：

```text
全部
```

---

## Audit 页面

普通用户：

```text
actor_user_id = current_user.id
OR
resource belongs to current_user
```

管理员：

```text
全部
```

---

# 26. User 禁用机制

用户 disabled 后：

```text
禁止登录
禁止创建新资源
禁止执行 Digital Employee
禁止创建任务
禁止创建 Webhook
禁止调用受保护 API
```

但是：

```text
已有 Audit
已有 Workstation
已有 Digital Employee
已有历史任务
```

必须保留。

Workstation 收到 Center Server 的用户禁用状态后：

```text
停止接受该用户的新任务
保持安全断开
```

---

# 27. Workstation 权限

Workstation 不再绑定单一 User。

Workstation 注册时需要记录：

```text
created_by_user_id
```

但真正的 User 访问权限通过：

```text
workstation_users
```

管理。

注册流程：

```text
User / Admin
 ↓
创建 Workstation Registration Token
 ↓
Workstation CLI
 ↓
register
 ↓
Center Server
 ↓
创建 Workstation
 ↓
建立 workstation_users 关系
```

例如：

```text
Workstation A
    │
    ├── User A (OWNER)
    ├── User B (MEMBER)
    └── User C (MEMBER)
```

Workstation 本身不能自行声明：

```text
“我是 User A 的机器”
```

Center Server 必须根据 Registration Token 和服务器端授权关系建立 User ↔ Workstation 关系。

如果后续允许管理员把已有 Workstation 分享给其他用户：

```text
Admin
 ↓
Workstation A
 ↓
添加 User B
 ↓
workstation_users
```

即可完成共享。


---

# 28. Workstation 注册安全

推荐：

```text
短期 Registration Token
+
一次性使用
+
设备绑定
+
Center Server 签发长期 Credential
```

流程：

```text
POST /api/workstations/registration-tokens
        ↓
Registration Token
        ↓
workstation register
        ↓
Center Server 验证
        ↓
创建 Workstation
        ↓
签发长期 Credential
```

---

# 29. Workstation Credential

Workstation 与 Center Server 通信使用：

```text
TLS
+
设备身份
+
Credential
```

如果当前架构已经使用：

```text
mTLS
```

继续保留。

否则第一阶段：

```text
TLS
+
Per-Workstation Credential
+
Request ID
+
Timestamp
+
Replay Protection
```

必须防止：

```text
消息窃听
消息篡改
重放攻击
Credential 重放
```

---

# 30. Digital Employee 权限

Digital Employee 不应拥有独立于 User 的“系统管理员权限”。

它的权限必须受：

```text
User Permission
+
Employee Configuration
+
Workstation Policy
+
Quota
```

共同限制。

例如：

```text
User A
  ↓
Code Employee
  ↓
只能访问 User A 授权的 Workstation
```

---

# 31. Employee → Employee 调用

现有设计允许：

```text
Employee A
    ↓ @
Employee B
```

必须加入权限检查。

调用链：

```text
Employee A
 ↓
Center Server
 ↓
验证 Employee A 所属 User
 ↓
检查 A 是否允许调用 B
 ↓
检查 B 是否允许被调用
 ↓
检查 Quota
 ↓
创建 Task / Session
 ↓
Employee B
```

第一版建议：

```text
默认只允许同一 User 下的 Digital Employee 互相调用。
```

跨用户调用需要显式授权。

---

# 32. Token 使用统计

必须能够统计：

```text
User
Workstation
Digital Employee
Provider
Model
Date
```

建议：

```text
token_usage
```

字段：

```text
id
user_id
workstation_id
digital_employee_id
provider
model
input_tokens
output_tokens
total_tokens
request_id
created_at
```

这样 Admin-Web 可以提供：

```text
Token 使用趋势
用户排行
工作站排行
数字员工排行
模型消耗
```

注意：

**排行仅用于运营统计，不应成为权限判断依据。**

---

# 33. 配额超限行为

当：

```text
User quota exceeded
```

应该：

```text
拒绝新请求
```

返回：

```http
429 Too Many Requests
```

错误码：

```text
QUOTA_EXCEEDED
```

如果是：

```text
Workstation quota exceeded
```

返回：

```text
WORKSTATION_QUOTA_EXCEEDED
```

如果：

```text
Digital Employee quota exceeded
```

返回：

```text
DIGITAL_EMPLOYEE_QUOTA_EXCEEDED
```

---

# 34. Admin-Web 权限矩阵

第一版推荐：

| 模块 | USER | OPERATOR | AUDITOR | SYSTEM_ADMIN |
|---|---|---|---|---|
| Dashboard | Own | All | All | All |
| 用户管理 | - | 查看 | 查看 | All |
| 工作站管理 | Own | All | - | All |
| 数字员工管理 | Own | All | - | All |
| 任务 | Own | All | 查看 | All |
| 自动化任务 | Own | All | 查看 | All |
| Token/Quota | 查看自己的 | All | 查看 | All |
| 审计 | Own | All | All | All |
| 系统监控 | Own | All | 查看 | All |
| 系统配置 | - | 部分 | - | All |

现有系统有的，但是这里没有的需要补上

最终权限以数据库中的 RolePermission 为准。

---

# 35. API 设计

统一：

```text
/api/v1
```

---

## 35.1 当前用户

```http
GET /api/v1/me
```

返回：

```json
{
  "user": {},
  "roles": [],
  "permissions": []
}
```

---

## 35.2 用户

```http
GET    /api/v1/users
POST   /api/v1/users
GET    /api/v1/users/:id
PATCH  /api/v1/users/:id
POST   /api/v1/users/:id/disable
POST   /api/v1/users/:id/enable
```

---

## 35.3 角色

```http
GET    /api/v1/roles
POST   /api/v1/roles
GET    /api/v1/roles/:id
PATCH  /api/v1/roles/:id
DELETE /api/v1/roles/:id
```

---

## 35.4 权限

```http
GET /api/v1/permissions
```

---

## 35.5 工作站

```http
GET    /api/v1/workstations
POST   /api/v1/workstations
GET    /api/v1/workstations/:id
PATCH  /api/v1/workstations/:id
DELETE /api/v1/workstations/:id
POST   /api/v1/workstations/:id/ping
```

---

## 35.6 Digital Employee

```http
GET    /api/v1/digital-employees
POST   /api/v1/digital-employees
GET    /api/v1/digital-employees/:id
PATCH  /api/v1/digital-employees/:id
DELETE /api/v1/digital-employees/:id
POST   /api/v1/digital-employees/:id/execute
```

---

## 35.7 Quota

```http
GET   /api/v1/quotas
POST  /api/v1/quotas
PATCH /api/v1/quotas/:id
```

---

## 35.8 Audit

```http
GET /api/v1/audit-logs
GET /api/v1/audit-logs/:id
```

后端自动 Scope Filter。

---

# 36. 数据库建议

核心表：

```text
users
roles
permissions
role_permissions

workstations
digital_employees

quota_policies
quota_usage

token_accounts
token_usage

audit_logs

registration_tokens
sessions
```

如果已有任务系统：

```text
tasks
automation_tasks
webhooks
calendar_tasks
scheduled_tasks
```

所有需要归属的数据必须明确 owner：

```text
owner_user_id
```

或者通过资源关系能够可靠推导。

---

# 37. 数据库约束

Workstation 与 User 是多对多关系，因此必须增加：

```text
FK workstation_users.workstation_id -> workstations.id
FK workstation_users.user_id -> users.id

FK digital_employees.owner_user_id -> users.id
FK digital_employees.workstation_id -> workstations.id
```

业务层必须强制：

```text
digital_employee.owner_user_id
```

对应的 User 必须拥有或被授权访问：

```text
digital_employee.workstation_id
```

即：

```text
User
  ↓
workstation_users
  ↓
Workstation
  ↓
Digital Employee
```

不能创建：

```text
User A
 └── Employee A
      └── Workstation B

如果 User A 没有 Workstation B 的访问权限
```

必须返回：

```text
WORKSTATION_ACCESS_DENIED
```


---

# 38. 删除策略

User 不建议默认物理删除。

推荐：

```text
ACTIVE
DISABLED
```

删除操作实际上：

```text
soft delete
```

例如：

```text
deleted_at
```

历史：

```text
Audit
Token Usage
Task History
Workstation History
Employee History
```

必须保留。

---

# 39. 数据迁移

现有单用户系统升级时：

### Step 1

创建默认 User：

```text
system-owner
```

### Step 2

将现有 Workstation：

```text
owner_user_id = system-owner.id
```

### Step 3

将现有 Digital Employee：

```text
owner_user_id = system-owner.id
```

### Step 4

创建：

```text
SYSTEM_ADMIN
```

### Step 5

给 system-owner 分配：

```text
SYSTEM_ADMIN
```

### Step 6

迁移历史 Audit / Task / Token 数据。

原则：

**不能因为增加 User 模型而丢失现有资源。**

---

# 40. Admin 初始化

首次部署：

```text
数据库初始化
        ↓
创建 SYSTEM_ADMIN Role
        ↓
创建全部 Permission
        ↓
创建默认 Admin User
        ↓
要求首次登录修改密码 / 完成认证
```

默认管理员账号不得写死在代码中。

---

# 41. 安全要求

## 41.1 密码

如果使用密码登录：

```text
Argon2id
```

禁止：

```text
MD5
SHA1
明文密码
```

---

## 41.2 Session

推荐：

```text
短生命周期 Access Token
+
Refresh Token
```

或者：

```text
Secure HttpOnly Cookie
```

禁止：

```text
localStorage 保存长期敏感 Token
```

如果 Admin-Web 是同源 Web 应用，优先考虑 HttpOnly Cookie。

---

# 42. CSRF

如果使用 Cookie：

必须：

```text
SameSite
CSRF Token
Origin Check
```

---

# 43. Audit 必须记录安全事件

至少：

```text
login.success
login.failed
logout

user.created
user.updated
user.disabled
user.enabled

role.updated
permission.updated

workstation.registered
workstation.deleted

digital_employee.created
digital_employee.updated
digital_employee.deleted
digital_employee.executed

quota.updated

token.created
token.revoked

task.created
task.executed

automation.created
automation.executed
```

---

# 44. Audit 不可被普通用户删除

普通用户：

```text
只能读自己有权限看到的 Audit
```

不能：

```text
delete audit
update audit
```

管理员也建议：

```text
Audit append-only
```

---

# 45. 数据查询层设计

推荐建立统一的 Scope Resolver：

```go
type ScopeResolver interface {
    Resolve(
        user User,
        permission string,
    ) ResourceScope
}
```

例如：

```go
scope := resolver.Resolve(
    currentUser,
    "workstation.read",
)
```

结果：

```text
ALL
OWN
ASSIGNED
NONE
```

然后 Repository 层负责应用过滤。

---

# 46. 推荐的后端分层

```text
HTTP Handler
     ↓
Middleware
     ↓
Auth
     ↓
Authorization
     ↓
Scope Resolver
     ↓
Service
     ↓
Repository
     ↓
Database
```

不要在 Handler 中写大量权限业务。

---

# 47. Authorization Middleware

推荐提供：

```go
RequirePermission("workstation.read")
```

以及：

```go
RequirePermission("workstation.update")
```

但 Permission 只解决：

```text
Can I do this?
```

Scope Resolver 解决：

```text
Which resources can I do this on?
```

---

# 48. Repository Scope

例如：

```go
ListWorkstations(ctx, scope)
```

而不是：

```go
ListAllWorkstations(ctx)
```

普通用户：

```text
scope = owner_user_id=current_user.id
```

管理员：

```text
scope = ALL
```

这样可以避免开发人员忘记过滤数据。

---

# 49. Admin API 与普通 API

不建议维护两套资源 API：

```text
/admin/workstations
/user/workstations
```

更推荐统一：

```text
/api/v1/workstations
```

权限系统自动决定返回范围。

这样：

```text
Admin
GET /api/v1/workstations
→ ALL

User
GET /api/v1/workstations
→ OWN
```

---

# 50. 用户注册流程

如果公司统一账号体系：

优先支持：

```text
SSO / OAuth / OIDC
```

如果第一期没有公司 SSO：

```text
Admin 创建用户
        ↓
发送邀请
        ↓
用户设置密码
        ↓
激活账户
```

不要默认开放：

```text
任何人自行注册
```

除非公司明确需要开放注册。

---

# 51. 未来组织模型

第一期不需要实现复杂组织架构，但数据库设计不要阻碍未来扩展。

未来：

```text
Company
 ├── Department
 │    ├── Team
 │    │    ├── User
 │    │    └── Workstation
 │    └── User
 │
 └── User
```

Scope 可以扩展：

```text
OWN
TEAM
DEPARTMENT
ALL
```

---

# 52. 第一阶段开发范围

Codex 第一阶段必须完成：

```text
1. User
2. Role
3. Permission
4. RolePermission
5. RBAC
6. Scope
7. Workstation 多用户共享模型
8. workstation_users
9. Digital Employee owner
10. Digital Employee → Workstation 授权关系
11. Digital Employee 独立工作目录
12. User Detail
13. User Management
14. Audit Scope
15. Token / Quota
16. Workstation/User/Employee 配额
17. API Authorization
18. Admin-Web 页面权限
19. 数据迁移
```

---

# 53. 第二阶段

```text
SSO / OIDC
组织架构
Department
Team
Project Scope
更复杂的 Quota
Token Secret Manager
成本统计
Billing
```

---

# 54. 验收标准

## 用户

- [ ] Admin 可以创建用户
- [ ] Admin 可以禁用用户
- [ ] Admin 可以启用用户
- [ ] 用户不能访问用户管理页面
- [ ] 用户不能通过 API 获取其他用户信息

## Workstation

- [ ] Workstation 必须属于 User
- [ ] User 只能看到自己的 Workstation
- [ ] Admin 可以看到所有 Workstation
- [ ] User 无法绑定其他 User 的 Workstation

## Digital Employee

- [ ] Digital Employee 必须属于 User
- [ ] Digital Employee 必须绑定 Workstation
- [ ] Employee 与 Workstation 必须属于同一 User
- [ ] User 只能管理自己的 Employee

## 权限

- [ ] 页面根据 Permission 显示
- [ ] API 强制 Permission Check
- [ ] API 强制 Scope Filter
- [ ] 前端隐藏不能绕过后端权限
- [ ] Admin 拥有 ALL Scope

## Audit

- [ ] 普通用户只能看到自己 Scope 内的日志
- [ ] Admin 可以看到全部
- [ ] Audit 不允许普通用户修改
- [ ] Audit 不允许普通用户删除

## Quota

- [ ] User 有独立 Quota
- [ ] Workstation 有独立 Quota
- [ ] Digital Employee 有独立 Quota
- [ ] 子资源不能超过父资源
- [ ] 超限返回明确错误码
- [ ] Token Usage 可追溯到 User / Workstation / Employee

---

# 55. Codex 实现要求

Codex 在开始编码前必须：

1. 阅读现有 Center Server。
2. 阅读现有 Admin-Web。
3. 阅读现有 Workstation。
4. 阅读现有 Digital Employee 模型。
5. 阅读现有数据库 Migration。
6. 阅读现有认证与 Session 实现。
7. 阅读现有 Audit。
8. 阅读现有 Task / Automation。
9. 阅读现有 Token 配置。

然后输出：

```text
当前架构分析
现有表结构
现有 API
现有权限实现
需要迁移的代码
数据库 Migration 计划
API 修改计划
Admin-Web 修改计划
Workstation 修改计划
测试计划
```

未经分析不得直接大规模重构。

---

# 56. Codex 实施顺序

推荐严格按照：

```text
Phase 1
数据库模型
    ↓
Phase 2
Migration
    ↓
Phase 3
Auth / User
    ↓
Phase 4
RBAC / Permission
    ↓
Phase 5
Scope Resolver
    ↓
Phase 6
Workstation Owner
    ↓
Phase 7
Digital Employee Owner
    ↓
Phase 8
Quota
    ↓
Phase 9
Audit Scope
    ↓
Phase 10
Admin API
    ↓
Phase 11
Admin-Web 用户管理
    ↓
Phase 12
所有现有 API 接入 Authorization
    ↓
Phase 13
测试
    ↓
Phase 14
数据迁移验证
```

---

# 57. 核心原则总结

整个系统最终应该遵循：

```text
                    User
                      │
              ┌───────┴────────┐
              ▼                ▼
           Role              Quota
              │
              ▼
         Permission
              │
              ▼
            Scope
              │
      ┌───────┼────────┐
      ▼       ▼        ▼
 Workstation Employee  Audit
      │       │
      └───────┴────────┘
              │
              ▼
         Token Usage
```

最重要的三个原则：

### 原则一

```text
Permission ≠ Scope
```

权限决定：

> 能不能做

Scope 决定：

> 能对谁做

### 原则二

```text
Frontend visibility ≠ Security
```

前端隐藏菜单只是用户体验。

真正安全必须在：

```text
Center Server API
```

执行。

### 原则三

```text
User 是资源归属的核心边界
```

所有：

```text
Workstation
Digital Employee
Task
Automation
Token
Quota
Audit
```

都必须能够可靠地关联到 User，或者通过资源关系可靠推导 User。

---

# 58. 最终目标

升级完成后，公司内可以形成：

```text
                    Company AI Employee Platform
                              │
               ┌──────────────┴──────────────┐
               │                             │
             Admin                         Users
               │                             │
        全局管理 / 审计                 用户级权限控制
               │                             │
        ┌──────┴────────┐            ┌───────┴────────┐
        ▼               ▼            ▼                ▼
      Users        Workstations   Workstation A   Workstation B
                        │             │                │
                ┌───────┼───────┐     │                │
                ▼       ▼       ▼     ▼                ▼
              User A  User B  User C Employee A      Employee B
                │       │       │
                ▼       ▼       ▼
              Employee Employee Employee
                │       │       │
                └───────┴───────┘
                        │
                        ▼
                  独立工作目录
                        │
                        ▼
                   Cursor / Codex
                        │
                        ▼
                    Quota / Token
                        │
                        ▼
                       Audit
```

最终实现：

**一个 Center Server 管理全公司用户、共享 Workstation、Digital Employee、权限和配额；一个 Workstation 可以被多个用户共享；每个 Digital Employee 明确归属于一个用户并绑定一个 Workstation；每个 Digital Employee 在 Workstation 上拥有独立工作目录和独立 Agent Session；用户只能访问自己被授权的 Workstation 和自己的 Digital Employee；管理员拥有全局管理能力；Token/Quota 分层控制；所有敏感操作都有 Audit；并为未来部门、团队、项目级权限预留扩展能力。**


特别说明：
1. **由于是在原有系统上新增以上需求，以上的表格字段都只是参考，原系统有的优先用原来的，有新增再加**
2. **Admin-Web 页面结构只管新增的用户相关的，系统中现有的不用管**
