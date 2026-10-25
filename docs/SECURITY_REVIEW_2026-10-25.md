# 外网部署安全评估

> 本文保留 `7f855ea` 的修复前审计记录。当前工作区修复与升级要求见 [安全修复说明](/Users/divad/Work/OpenSource/ai-employee-platform/docs/SECURITY_HARDENING_2026-10-25.md)；修复尚未在真实部署环境完成验收。

评估日期：2026-10-25  
代码位置：`/Users/divad/Work/OpenSource/ai-employee-platform`  
分支：`codex/pve-software-management`  
提交：`7f855ea`

## 1. 结论与范围

**当前版本不适合按仓库默认示例直接对公网开放，也不具备可靠的多用户隔离边界。** 最需要处理的是飞书派发与审批授权、MCP 范围检查、工作站事件身份绑定，以及数据库与初始化接口的部署边界。HTTPS、反向代理和登录密码不能单独修复这些业务越权。

本次进行了源码、Compose、工作站接入、飞书、MCP、会话、备份和基础设施模块审查，并用隔离内存对象复现部分授权缺陷。审查开始时工作区干净；本次只新增这份报告，没有修改业务代码、账户、权限、配置、镜像或运行数据。所有结论针对上述提交，不代表其他分支的状态。

当前 Docker daemon 无法连接，常用服务端口未发现本机监听；未获得公网部署地址，也没有进行公网扫描。因此，下面的“暴露”描述是代码与配置形成的攻击面，**不表示已确认某个线上实例正在被公网访问或已被入侵**。实际还取决于防火墙、反向代理、环境变量、数据库是否已初始化及飞书应用可见范围。

严重程度采用定性分级，不给未经完整环境测量的 CVSS 分数：

| 等级 | 判断依据 |
| --- | --- |
| 严重 | 满足所列前提后，可接管管理员、越权驱动员工，或越过高风险操作的人类审批边界 |
| 高 | 可跨用户读写/使用资源、伪造任务状态、泄露凭据，或造成服务不可用 |
| 中 | 有条件削弱防护、审计、协议校验或运维边界，需要与其他条件组合 |

## 2. 外网攻击面

| 入口 | 默认/示例情况 | 风险暴露与前提 |
| --- | --- | --- |
| `8088` 管理页面与 `/api/` | Docker 发布到所有宿主机网卡；Nginx 仅监听 HTTP | 网络可达时可访问登录、初始化、飞书 HTTP 回调和其他 API。登录后的 API 仍存在下文的资源授权问题 |
| `8080` 控制面 HTTP | Docker 发布到所有网卡；Go 使用 `ListenAndServe` | 除 `/api/` 外还可直达 `/mcp`、`/metrics`，可能绕过外层网关的访问限制和限流 |
| `9090` 工作站 gRPC | Docker 发布到所有网卡 | 已使用 TLS 1.3 和双向证书认证；持有合法工作站证书后仍可触发下文的事件越权 |
| `5432` PostgreSQL | 不设变量时绑定回环；示例 `.env` 设置 `POSTGRES_PORT=5432` | 示例变量会把发布规则展开成 `5432:5432`，监听所有网卡；新建数据库示例密码为固定值。是否公网可达还取决于防火墙 |
| 飞书机器人 | 可通过长连接收消息 | 即便服务器不公开 HTTP 回调，飞书应用可见范围内的用户仍可进入派发与审批链路 |
| SFTP / SSH / PVE / 软件源 | 控制面主动出站 | 管理权限、备份信任或供应链失守时，影响可扩展到备份、工作站和基础设施 |

证据：[Compose](/Users/divad/Work/OpenSource/ai-employee-platform/deploy/docker-compose.yml:10)、[环境示例](/Users/divad/Work/OpenSource/ai-employee-platform/deploy/.env.example:14)、[Nginx](/Users/divad/Work/OpenSource/ai-employee-platform/deploy/nginx.admin.conf:7)、[HTTP 与 MCP 挂载](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:619)。

## 3. 严重风险：公网开放前应优先修复

### R01. 示例配置可公开数据库，并使用固定初始密码

**等级：严重，有部署前提。**

Compose 中 `${POSTGRES_PORT:-127.0.0.1:5432}:5432` 的安全缺省值，被 `.env.example` 的 `POSTGRES_PORT=5432` 覆盖。按示例复制环境文件后，实际变成所有网卡发布。`POSTGRES_USER`、`POSTGRES_PASSWORD`、`POSTGRES_DB` 的示例/缺省值均为 `aie`。

如果是使用该密码初始化的新数据库，而且宿主机/云防火墙允许访问，外部攻击者可直接读写整个平台数据库。已有持久卷可能已使用不同密码，本次没有验证实际数据库凭据。

数据库内还存储可直接使用的 Web 会话 token。结合 R08 的固定加密密钥，数据库或备份泄露可能进一步暴露管理员会话、TOTP 种子及其他机密。

修复：移除数据库宿主机端口发布，或明确使用独立 `POSTGRES_BIND_IP=127.0.0.1`；生产启动强制随机强密码；数据库仅允许应用内网连接。

证据：[数据库发布与密码](/Users/divad/Work/OpenSource/ai-employee-platform/deploy/docker-compose.yml:10)、[覆盖缺省值的示例](/Users/divad/Work/OpenSource/ai-employee-platform/deploy/.env.example:14)。

### R02. 未初始化实例可被远程抢先创建超级管理员

**等级：严重，仅在数据库未初始化时成立。**

`POST /api/setup/init` 无需登录，也没有一次性部署密钥或来源限制。它只检查系统是否已初始化，随后按请求创建超级管理员并返回会话。`GET /api/setup/status` 可公开查询是否仍需初始化。进程内互斥锁不能证明请求者是部署者。

新装、数据库重建或指向空库后，若先开放公网，最先访问者可取得控制权。已完成初始化的数据库会拒绝重复初始化，不能据此认定现有实例可以直接被重新接管。

修复：部署时生成独立一次性 bootstrap secret，通过受保护通道提供；初始化仅允许本机或安装阶段；数据库层保证一次性初始化事务。

证据：[公开初始化路由](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/router.go:106)、[初始化处理](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/router.go:383)。

### R03. 飞书派发缺少员工访问授权，并可抢占首次绑定

**等级：严重；攻击者需要能向机器人发消息。已隔离复现。**

发现两条独立路径：

1. 员工没有飞书绑定时，首次 `@` 会自动把发送者绑定到员工；随后发送者可被映射成员工所属的平台用户，创建任务。
2. 已绑定自己员工的发送者，也可指定其他用户的员工。派发仅检查目标员工可用，没有验证发送者是否有权使用该员工/工作站。

影响包括越权驱动其他员工、消耗配额、访问 Agent 可读的工作区与会话上下文，并将结果发送到发起会话。具体能执行什么命令仍取决于工作站账号、Agent 沙箱和工具权限。本次没有执行真实命令或读取真实秘密。

飞书 SDK 认证了事件来源，不能代替平台对“这个发送者能用哪个员工”的授权。该问题在飞书长连接模式也存在。

修复：取消首次提及即绑定；通过登录用户生成配对码完成平台用户与 OpenID 绑定；每次派发检查员工/工作站 ACL；群聊通知目标也应有访问约束。

证据：[自动绑定](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/feishu/service.go:970)、[先绑定再派发](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/feishu/service.go:1405)、[创建任务](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/feishu/bridge.go:84)、[平台身份映射](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/feishu/bridge.go:166)。

### R04. 同群其他成员可确认 Agent 的审批请求

**等级：严重，取决于审批请求内容；已隔离复现。**

待处理 Inquiry 按 ChatID 查找。群成员回复选项编号/名称后，系统直接确认，未验证回复者是否为任务发起者或获授权审批人。卡片按钮路径虽然记录点击者 OpenID，也没有执行审批者授权。

当前 Codex 已采用 `approvalPolicy: on-request`。如果该请求代表命令执行、文件修改或权限升级，其他群成员可替真正授权人批准；能在群里看到卡片不应自动获得批准权限。

修复：Inquiry 保存允许审批的用户身份，卡片和文本确认共同调用同一授权检查；校验选项属于该请求；检查任务归属、请求有效期和一次性状态；高风险升级避免默认使用群内文本数字确认。

证据：[文本回复](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/inquiry/service.go:249)、[确认与下发](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/inquiry/service.go:292)、[Codex 审批策略](/Users/divad/Work/OpenSource/ai-employee-platform/workstation/internal/providers/codex/appserver.go:166)。

## 4. 高风险

### R05. `/mcp` 忽略 OWN 范围，普通用户可修改全局资源

**等级：高；需要普通账号及可访问的 `/mcp` 入口。已隔离复现。**

默认 USER 拥有 `workflow.write`，范围为 OWN。MCP 的 Web 会话认证仅调用 `Authorize(workflow.write)`，将其转成布尔 `CanWrite`，没有保留或检查 OWN/ALL。后续工具只检查这个布尔值。

普通用户因此可操作共享知识、技能和工作流；`sync_skill` / `sync_all_skills` 还接受任意 workstation_id/employee_id，调度层未补充所有权检查。本次复现了删除共享知识、将外部工作站 ID 传给同步接口；未实际向真实工作站推送技能。恶意技能可能影响后续 Agent 执行，未据此宣称已验证远程代码执行。

对应的共享 MCP 服务 REST 写接口已有 `onlyAll`，本次确认普通用户会收到 403；漏洞主要在另一条 `/mcp` 授权链和 R06 的绑定接口。

修复：统一 REST/MCP 的权限码与范围检查，按工具检查资源及同步目标；员工 scope 计算失败应拒绝请求，不能把 nil 当作不限制。

证据：[会话转为 CanWrite](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/mcpserver/server.go:145)、[工具写权限判断](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/mcpserver/tools.go:54)、[同步入口](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/mcpserver/tools.go:192)、[同步调度](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/scheduler/workflow_dispatch.go:37)。

### R06. MCP 绑定存在跨员工修改与跨用户凭据引用

**等级：高；需要账号以及目标 binding/credential ID。已隔离复现。**

修改/删除接口先校验 URL 中的员工属于自己，再直接操作另一个 `bindingId`，没有验证该绑定确实属于 URL 员工。创建绑定只检查员工权限，未检查 `CredentialID` 是否属于当前用户。

本次确认：用自己的员工 URL 可禁用其他员工的绑定；将其他用户凭据绑定到自己的员工后，运行时可解析该凭据。调度器随后会把解析出的认证头/环境变量注入工作站。随机 ID 增加猜测难度，但不能替代资源授权。

修复：读取 binding 后同时核验 employee_id、owner、server 和 credential 的关系；凭据引用需要所有权或显式共享授权；服务层接收并校验 actor，避免只靠路由防护。

证据：[创建与修改绑定](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/mcp_handlers.go:258)、[删除绑定](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/mcp_handlers.go:312)、[运行时凭据注入](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/scheduler/workflow_dispatch.go:184)。

### R07. 合法工作站可伪造其他节点/任务的事件

**等级：高；需要合法节点证书或控制一台工作站。已隔离复现入口接受行为。**

gRPC 已认证节点 wsID，但仅在事件 workstation_id 为空时填入 wsID，非空且不同的 ID 不会被拒绝。业务 OnEvent 也未验证 job_id 所属工作站是否等于证书身份。

恶意节点可提交其他任务的结果、进度、成功/失败事件，影响时间线、通知、用量及事件序号。这里的缺陷是认证后的资源授权，不能用 mTLS 本身加密来弥补。复现使用模拟流，未改动任何真实任务。

修复：事件节点 ID 必须等于证书身份；Job/Session/Employee 必须属于该节点且关联一致；用服务器分配信息验证所有 worker 事件和确认消息。

证据：[事件入口](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/workergrpc/server.go:214)、[事件分发](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:307)、[任务结果处理](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:389)。

### R08. 加密主密钥可静默退回源码中的固定值

**等级：高；需要取得密文、数据库、数据卷或备份。**

Secrets 在 `AIE_SECRET_MASTER_KEY` 为空时使用固定字符串派生密钥。备份另有独立密钥路径：`AIE_MASTER_KEY` / `AIE_BACKUP_MASTER_KEY` 均为空时，也使用固定字符串。Compose 没有转发后两个变量，仅设置 Secrets 密钥也不能解决备份密钥问题。

这会使“已加密”的机密、TOTP 种子、备份目标凭据及备份包，在存储泄露后可能被解密。完整备份包含 CA 目录和 Secrets 目录，泄露半径较大。不能把这个问题描述成没有密文也能远程解密。

修复：生产环境无有效随机密钥时拒绝启动；明确区分并传递两套密钥；将 CA 私钥与业务备份分层保护。已有数据必须经过解密/重加密迁移，直接更换变量可能导致旧数据无法读取。

证据：[Secrets 默认值](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/secret/vault.go:51)、[备份默认值](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/backup/crypto.go:32)、[完整备份内容](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/backup/snapshot.go:80)。

### R09. 工作站注册可使用明文 HTTP，一次性令牌存在并发复用

**等级：高；明文接入需要网络中间人，并发复用需要持有有效注册令牌。令牌复用已隔离复现。**

注册 CLI 允许通过 HTTP 提交 token/CSR，并直接信任响应中的 CA 与客户端证书。若跨公网使用明文 HTTP，中间人不仅能截获令牌，也可能替换首次建立的信任根。

服务端对令牌采用“读取、检查、签证书、标记已用”分离步骤，并忽略 MarkUsed 错误。并发请求可以在消耗前读取同一令牌。本次确认同一 token 能签发两个不同工作站身份。令牌默认有效 24 小时且没有最大 TTL，当前主程序仍使用内存 token store。

CA 会阻止给已有有效证书的工作站 ID 重复签发，因此不能据此断言可无条件冒充任意活跃节点。

修复：公网注册强制可信 HTTPS；必要时预置/核验 CA 指纹；令牌原子持久化消耗，绑定服务器分配的节点身份/公钥，缩短有效期并处理存储错误。

证据：[注册客户端](/Users/divad/Work/OpenSource/ai-employee-platform/workstation/internal/app/app.go:499)、[令牌消耗](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/enrollment/service.go:87)、[内存存储接线](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:157)。

### R10. 管理会话明文落库，密码重置和角色降级不立即撤权

**等级：高；需要已持有有效会话或可读取会话表。撤权问题已隔离复现。**

Web bearer token 原样存储在数据库。认证使用会话内保存的角色；改密码、修改用户角色时没有撤销既有会话。因此旧管理员降级后，原会话仍保留旧角色权限；重置密码也不会自动令泄露的旧 token 失效，会话通常有效 12 小时。

用户查询返回空或错误时，Authenticate 还可能继续接受已有会话，应该改成拒绝。禁用仍能被查询到的用户有检查，这项保护已经存在。

前端将 token 放在 localStorage，会放大未来 XSS 的影响；本次没有发现足以确认 XSS 的执行链。TOTP 为可选项，当前未核验真实管理员是否启用。

修复：数据库仅保存 token 哈希；密码/角色/禁用/删除等事件统一撤销会话或递增 session version；每次请求加载最新用户状态/角色；查询失败拒绝认证；公网管理员强制 MFA。

证据：[会话落库](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/database/web_session_store.go:27)、[认证与角色使用](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/auth/service.go:219)、[密码和角色修改](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/rbac_handlers.go:508)。

### R11. HTTP、回调和解压缺少统一资源上限

**等级：高；部分路径在认证前可达，导入/恢复路径需要相应权限。**

Go HTTP Server 未配置 ReadHeaderTimeout 等服务端超时。飞书 HTTP 回调在验证签名前就无上限 `io.ReadAll`；Nginx 的所有 `/api/` 共用高达 514 MB 的请求体限制，直接 8080 入口还可绕过该限制。

工作流导入的 `ParseMultipartForm(64 MiB)` 是内存阈值，不是总上传大小限制；ZIP 条目解压又无累计大小/文件数上限。备份加解密及恢复也多处整包读入内存。大请求、慢连接和压缩炸弹可能耗尽内存/磁盘，造成服务不可用。本次没有实施压力或拒绝服务攻击。

修复：按路由设置 MaxBytesReader、读取/连接超时和并发上限；设置解压后累计大小、条目数量和压缩比限制；大备份采用受限流式处理。

证据：[HTTP Server](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:623)、[回调先读请求体](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/feishu_handlers.go:208)、[ZIP 导入](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/workflow_handlers.go:405)、[恢复整包读取](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/backup/restore.go:198)。

### R12. 备份 SFTP 不验证主机身份，恢复未强制完整性与加密状态

**等级：高；需要网络中间人或控制备份存储，恢复攻击还需要授权用户执行恢复。**

SFTP 使用 `ssh.InsecureIgnoreHostKey()`。中间人可冒充备份服务器；若使用密码认证，可获得发送给假服务器的密码，并读取/篡改上传内容。公钥认证不会直接传出 SSH 私钥，不能将其误写成私钥必然被窃取。

恢复流程读取远程文件后未先比对记录的 run.Checksum，仅依据文件 magic 判断是否加密；没有 magic 时直接按明文压缩包处理。即使原备份应加密，也可替换为另一个结构合法的明文包。备份替换者因此可能在管理员恢复时污染数据库、CA 和 Secrets。已有恢复权限及二次认证限制降低了直接利用范围。

修复：固定 SFTP host key/known_hosts；恢复前必须校验预期摘要及可信清单；按备份记录强制验证加密算法，禁止降级接受明文；把恢复校验与实际恢复共用不可绕过的校验函数。

证据：[SFTP 校验关闭](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/backup/destination/sftp.go:72)、[恢复格式判断](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/backup/restore.go:180)。

## 5. 中风险及防护缺口

### R13. 审计和部分安全状态只在内存，USER 权限被启动逻辑重新扩充

主程序固定使用内存审计、权限策略、Secret 绑定和部分审批 store。即使业务数据库启用，这些状态也未全部持久化；重启会丢失相关审计或配置，削弱追责和策略持续性。

另有启动 SQL 对 USER 角色重新赋予所有 permissions，范围 OWN。手工收紧该内置角色可能在下次启动时被恢复；OWN 又必须由每条调用链正确落实，R05 正是未落实的实例。

修复：安全状态和审计持久化；审计外送并设置保留策略；把默认角色种子改为一次性迁移，避免覆盖管理员调整；按最小权限重新设计 USER。

证据：[内存审计](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:72)、[其他内存安全 store](/Users/divad/Work/OpenSource/ai-employee-platform/server/cmd/server/main.go:434)、[启动覆盖权限](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/database/db.go:80)。

### R14. 自定义飞书 HTTP 签名与重放控制不完善

仅针对自定义 HTTP Webhook 路径：签名计算使用 VerificationToken，官方 SDK 的对应签名计算使用 EncryptKey；并且未检查 timestamp 时效或 nonce 一次性。无签名头时还允许通过静态 token 校验。当前实现会拒绝无凭证请求，因此不是“匿名 webhook 任意执行”。

这些问题可能造成官方签名事件被拒、令牌泄露后被伪造，以及缺乏协议层的新鲜度保证。现有事件去重和任务幂等能挡住部分重复事件，不能概括成所有旧事件都一定重复执行。长连接 SDK 的认证与该自定义 HTTP 校验是两条路径。

修复：使用官方协议/SDK 完成校验解密，生产禁用静态 token 降级，加入合理时间窗和持久化去重。

证据：[当前签名算法](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/feishu/service.go:1490)、[HTTP 认证分支](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/feishu_handlers.go:224)、[飞书官方 SDK 对应实现](https://github.com/larksuite/oapi-sdk-go/blob/v3.12.0/event/event.go)。

### R15. 代理头无条件信任，监控指标无需认证

`clientIP()` 无条件信任调用者提供的 X-Real-IP / X-Forwarded-For。直接开放 8080 时可伪造审计来源，并影响基于 IP 的限流。现有登录用户名限流/账户锁定仍存在，不能据此认为能完全绕过登录防护。

`/metrics` 无认证，可查看在线工作站、活跃会话、任务数量及耗时等运营信息；未发现该端点直接返回机密。默认 admin Nginx 不代理 `/metrics`，主要涉及直接控制面端口。

修复：仅信任明确配置的代理网段，并在边缘覆盖转发头；监控入口只对内网或监控系统开放。

证据：[来源 IP](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/router.go:1555)、[指标路由](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/router.go:320)。

## 6. 基础设施与供应链：高权限的影响半径

这一部分是部署信任与权限设计风险，不是已证明的匿名 SSRF 或命令注入。

- 平台保存 SSH/PVE/备份凭据，并能部署软件、打开终端。若使用 root SSH 或范围过大的 PVE Token，控制面或高权限管理员失守可扩展到虚拟化平台和工作站。应使用专用非 root 用户、限制 sudo 命令及 PVE ACL，并隔离管理网络。
- 部分集群、主机、软件源和 release 修改仅要求管理员范围，没有二次认证。攻击者持有管理员会话后，可能先改变后续操作的目标/来源。应对信任根、下载源和凭据关系变更增加二次认证与审计。
- 软件包已有 HTTPS 和 SHA256 校验，但发布信息与摘要仍可能来自同一被信任的软件源。上游源失守时，摘要本身不能证明发布者身份。宜增加发布签名、公钥固定、来源允许列表和版本固定。
- Compose 使用 latest/可变标签，基础镜像也未固定 digest；部署制品应生成 SBOM、做镜像/依赖漏洞扫描，并固定发布版本。此次未完成镜像与整个传递依赖集合的 CVE 扫描，不将未核实的 CVE 当作项目漏洞。

证据：[基础设施路由权限](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/api/infrastructure_handlers.go:21)、[软件源下载](/Users/divad/Work/OpenSource/ai-employee-platform/server/internal/infrastructure/packages.go:127)、[镜像构建](/Users/divad/Work/OpenSource/ai-employee-platform/deploy/Dockerfile.admin:4)。

## 7. 已有有效防护与审查边界

现有代码包含多项有效措施，应保留并补齐调用链：

- 大量 REST 写操作已经要求 ALL 范围；普通用户修改共享 MCP Server 的 REST 请求被正确拒绝。
- 密码使用哈希，登录已有频率限制/锁定；启用 TOTP 后，登录及二次认证会检查动态口令。
- gRPC 使用 TLS 1.3 双向证书认证；工作站附件上传有证书、签名和任务归属检查。
- PVE 客户端要求 HTTPS 并验证证书；基础设施 SSH 执行链校验已配置的主机指纹。备份 SFTP 的缺陷不能泛化为所有 SSH 链路都未校验。
- 软件包部分流程已校验摘要和解包路径；命令构造含 shell quoting。当前前端是构建后的 Nginx 静态站点，不能直接将 Vite 开发服务器漏洞套到该生产入口。

本次没有证明匿名任意命令执行、SQL 注入、前端 XSS 或全部可配置 URL 都构成越权 SSRF。未验证这些问题，不等同于保证整个项目不存在这些问题。

## 8. 隔离复现记录

使用 Go overlay 将临时审查代码映射到测试路径，测试文件保存在 `/private/tmp/aie-security-audit-20261025`，没有写入仓库业务包。运行对象为内存 store、模拟流、假同步器和假命令接口，没有调用真实飞书、SSH、PVE 或工作站。

| 场景 | 结果 |
| --- | --- |
| 普通 USER 修改全局 MCP Server REST 配置 | 正确返回 403，排除该路径的误报 |
| 用自己的员工路径修改外部 binding | 确认可修改 |
| 绑定其他用户 credential 并解析认证 | 确认可使用 |
| OWN 范围会话通过 `/mcp` 删除共享知识 | 确认可删除 |
| `/mcp` 同步传入其他工作站 ID | 确认被转发至模拟同步器 |
| 飞书首次提及未绑定的外部员工 | 确认自动绑定并创建任务 |
| 已绑定其他员工的飞书用户派发到外部员工 | 确认可创建任务 |
| 同群未授权成员回复审批选项 | 确认请求被解决 |
| 单个注册 token 并发注册两个节点身份 | 确认两次签发成功 |
| 合法节点上报另一个节点/任务 ID | 确认事件被入口接受并转发 |
| 管理员降级、密码重置后的旧会话 | 确认旧权限/旧 token 仍有效 |

这些用例的 PASS 表示**成功复现当前不安全行为**，不是安全验收通过。没有运行真实破坏性操作、压力测试或外网攻击。

## 9. 修复顺序与上线要求

### P0：开放公网前完成

1. 关闭数据库公网发布、取消固定密码；初始化流程增加部署者证明。
2. 只通过可信 HTTPS 网关开放必要 Web 入口；阻断直接 8080 访问；工作站注册强制 HTTPS；9090 限制工作站来源。
3. 修复飞书账号配对、目标员工 ACL 和审批者身份验证。
4. 修复 MCP 的 OWN/ALL 与绑定/凭据关系授权。
5. 强制节点证书身份、事件身份与任务分配关系一致。
6. 生产强制随机密钥并安全迁移已有密文；若凭据曾在不安全环境暴露，执行轮换与会话撤销。

### P1：正式持续运行前完成

1. 注册 token 持久化、原子消耗；会话哈希存储与统一撤权；管理员强制 MFA。
2. 请求体、解压、并发、超时和备份内存上限。
3. SFTP 主机指纹、恢复强制完整性/加密校验。
4. 审计与安全策略持久化，移除启动时强制恢复宽权限。
5. 飞书标准签名与新鲜度验证，可信代理网段、监控入口隔离。
6. PVE/SSH 最小权限、软件发布签名、固定镜像版本和供应链扫描。

建议上线拓扑：公网仅暴露 HTTPS 网关；控制面和数据库在内网；工作站 gRPC 使用独立受限入口；PVE/SSH/备份存储在管理网络。网络隔离降低可达性，业务授权修复负责阻止已登录用户、飞书成员和已注册节点之间的横向越权，两者需要同时完成。
