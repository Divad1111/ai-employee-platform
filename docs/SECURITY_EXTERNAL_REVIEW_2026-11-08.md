# 外网部署安全审查（独立复核）

后续状态：本报告记录修复前的问题，修复实现、回归验证及升级要求见 [安全修复说明](SECURITY_FIXES_2026-11-08.md)。

日期：2026-11-08；审查基线：`41c2555`。本报告补充现有加固报告，不沿用旧报告“全部处理”的结论。

结论：暂不建议作为多用户控制平台直接向公网开放。存在已用本地测试复现的资源越权及凭据外发问题；应先修复下列高风险项。未发现并证实匿名直接接管控制面的攻击链，但这不等于不存在此类漏洞。

审查范围：Go 控制面、工作站关键执行路径、React 会话存储、Compose/Caddy/Nginx/CI 配置；结合静态检查、现有测试、临时本地安全探针和依赖扫描。没有连接线上 PVE/SSH/飞书、没有扫描实际公网入口、没有运行真实 Agent 执行攻击任务。没有修改业务实现或部署配置。

## 1. 高：员工与工作区可被改派到未授权工作站（已复现）

- 位置：`server/internal/api/router.go:859`、`:891`、`:1020`；`server/internal/scheduler/service.go:145`。
- 创建员工时会调用 `ensureWSAccessForEmployee`；更新员工时只检查原员工是否属于调用者，直接接受新的 `workstation_id`，没有复核目标工作站权限。
- 更新工作区同样只检查旧对象，直接保存新的工作站 ID。
- 本地探针使用仅有 `employee.write:OWN`、`workspace.write:OWN` 的用户，没有授予目标节点任何成员资格。两种改派均返回 HTTP 200，并实际改变存储记录。
- 调度器从员工记录读取工作站 ID，没有再次检查提交者对目标节点的授权。结合任务创建权限与在线目标节点，存在跨节点派发任务的风险；本次未在真实节点执行命令。
- 修复：对所有新增、修改、绑定和派发路径统一校验目标工作站权限；校验 employee/workspace/job 的工作站归属一致；派发前重新授权，避免撤权后排队任务继续执行。

## 2. 高：绑定自己的工作区可以修改其他用户的员工（已复现）

- 位置：`server/internal/api/router.go:1045`；`server/internal/workspace/service.go:109`。
- `POST /api/workspaces/{id}/bind` 校验调用者能否修改工作区，却未校验请求体中的 `employee_id`。
- `BindEmployee` 会解绑目标员工原工作区，并通过桥接器修改目标员工记录。
- 本地探针：本人范围用户将自己可写的工作区绑定给 `OwnerUserID` 不同的员工，返回 HTTP 200；目标员工 `WorkspaceID` 实际被改写。
- 影响：破坏他人的任务环境、替换其后续任务使用的目录；若被替换目录中有恶意项目配置，可能进一步影响 Agent 行为。
- 修复：同时授权工作区、目标员工、旧绑定员工以及两端工作站；在同一事务中修改关联。员工创建/修改和工作区创建中的关联字段也应采用同一校验。

## 3. 高（有前提）：共享工作站缺少平台级文件系统隔离

- 位置：`server/internal/workspace/service.go:82`；`workstation/internal/runtime/managers.go:158`；`workstation/internal/providers/codex/appserver.go:166`；`workstation/internal/acp/stdio.go:141`。
- 工作区路径只要求非空；工作站直接对其 `MkdirAll` 并作为 Provider 的工作目录，没有按用户/员工限制根目录或校验符号链接。探针已确认 API 接受路径 `/`。
- Codex 使用 `workspace-write`，但该 workspace 本身来自上述路径；其他 Provider 的保护也不能替代平台的租户隔离。ACP 子进程还继承守护进程环境变量。
- 前提：给不完全可信用户工作区编辑和任务执行权限，或处理可操纵 Agent 的外部内容。影响受工作站运行账号和 Provider 限制，不意味着无条件取得 root。
- 修复：由服务端分配工作目录；工作站独立验证规范化路径、符号链接及授权根目录；租户采用独立 OS 用户/容器/VM，进程环境使用白名单。不要在员工执行账号下保存其他租户的凭据。

## 4. 高：修改 PVE 目标可绕过凭据查看权限并外发原密钥（已复现）

- 位置：`server/internal/api/infrastructure_handlers.go:35`；`server/internal/infrastructure/service.go:114`、`:143`、`:151`；`server/internal/infrastructure/pve.go:61`。
- 集群修改与同步只要求 `infra.manage:ALL` 加 step-up；明文查看另设 `infra.credential` 权限。但管理者可以同时替换 URL/CA、保留原 `SecretID`，然后调用同步。服务端解密原 Token 并把它放入请求的 Authorization 头。
- 本地探针：无 `infra.credential` 的管理角色修改集群地址为本地测试 TLS 端点，配置测试 CA；同步后，该端点收到完整合成 Token。没有使用真实凭据或外部服务器。
- HTTPS 校验在此不能阻止攻击，因为目标地址与信任 CA 都由该调用者控制。
- 修复：凭据绑定经批准的目的地址/证书；目标或 CA 变更须凭据所有者授权或重新提供凭据，禁止沿用原密钥；限制控制面出站目的地。SSH 密码凭据的地址/指纹可变更模式也应按同样边界检查。
- 出站目标控制可参考 [OWASP SSRF Prevention](https://cheatsheetseries.owasp.org/cheatsheets/Server_Side_Request_Forgery_Prevention_Cheat_Sheet.html)。

## 5. 高：撤权/禁用用户不能终止现有 SSH 终端（代码确认）

- 位置：`server/internal/infrastructure/ssh.go:225`、`:246`；`server/internal/api/infrastructure_handlers.go:372`。
- 创建票据时检查权限；消费票据只检查存在、到期和一次性。随后处理终端输入时没有复核用户启用状态、Web 会话或 `infra.console` 权限。
- 已建立的活跃终端可持续到 30 分钟总超时；修改密码、注销、禁用用户或撤销角色不会由当前实现主动中断。未消费的票据在 30 秒有效期内也没有重新授权。
- 影响：账号泄露后，后台撤权不能及时阻止攻击者继续使用 SSH 账号的权限。
- 修复：票据绑定会话和凭据版本；消费时重新授权；维护连接注册表，在禁用/注销/撤权时断开，并周期复核。操作审计应覆盖会话关闭与撤销事件。

## 6. 中高：高权限自定义角色不受生产强制 MFA 约束（已复现）

- 位置：`server/internal/auth/service.go:250`、`:983`；`server/internal/mcpserver/server.go:156`。
- `AdminRoles` 只识别固定名称 `ADMIN` 和 `SUPER_ADMIN`。自定义角色即使拥有全局基础设施或其他管理权限，`EnforceAdminMFA=true` 时仍不要求 MFA。
- 本地探针中自定义 `infra.manage:ALL` 用户的 `MFARequired=false`；未绑定 TOTP，密码 step-up 即可执行第 4 项操作。
- 修复：按有效高风险权限判断 MFA 要求，或对所有管理后台用户统一强制 MFA；REST、MCP、角色变更和存量会话采用一致策略。

## 7. 中：二次认证密码校验没有登录同级的限流（代码确认）

- 位置：`server/internal/api/security_handlers.go:346`；`server/internal/auth/service.go:320`、`:345`。
- 登录有 IP/用户名窗口限流和失败锁定；`VerifyPassword` 与 `StepUp` 直接执行 bcrypt 校验，没有相应失败计数和速率限制。
- 前提：攻击者已经持有有效会话。可对当前账号密码持续猜测，或以高频 bcrypt 请求消耗 CPU。全局 32 个并发槽不能替代账号/IP 的速率限制；已有 TOTP 仍构成额外障碍。
- 修复：登录、step-up、TOTP 注册/重绑等复用认证限流策略；同时限制账号、会话、IP 和全局计算预算，并记录失败审计。

## 8. 中（纵深防御）：长期 Bearer Token 放在 localStorage，未配置 CSP

- 位置：`admin/src/api/client.ts:10`；`server/internal/auth/service.go:156`；`deploy/Caddyfile:2`；`deploy/nginx.admin.conf:10`。
- Web token 保存在 JavaScript 可读取的 localStorage，会话期限为 12 小时；默认网关没有 Content-Security-Policy。
- 本次未发现并复现 XSS。因此这里是风险放大因素，不是已证实的 XSS：一旦页面脚本或前端依赖被利用，Token 可被读走并在其他客户端重放。
- 修复：考虑 `HttpOnly; Secure; SameSite` Cookie 会话并同步补齐 CSRF 防护；配置适配 Monaco/Worker 的 CSP，先以 Report-Only 验证，再强制执行。参见 [OWASP HTML5 Security](https://cheatsheetseries.owasp.org/cheatsheets/HTML5_Security_Cheat_Sheet.html)。

## 9. 中：CI 漏洞扫描在错误目录运行（已复现）

- 位置：`.github/workflows/ci.yml:53`。
- 仓库根目录只有 go.work，没有 go.mod；该步骤直接运行 `govulncheck ./server/... ./workstation/...`。
- 使用当前扫描工具复现返回 `govulncheck: no go.mod file` 和退出码 1，没有执行扫描。这里会导致安全 job 失败，并非静默扫描通过。
- 修复：分别在 server 和 workstation 目录执行 `govulncheck ./...`；将成功完成的 security job 设置为合并门禁。

## 部署配置的剩余风险

以下属于上线配置与隔离要求，不作为已确认可利用漏洞：

- Caddy 默认将所有 `/api/*` 和 `/mcp` 暴露在同一公网域名，包含管理、PVE/SSH、备份接口。建议管理入口放到 VPN/访问代理后，公网只开放业务确需的 Webhook/MCP 路径；若必须公开整个后台，应先修复上述问题。
- 原生启动默认 `AIE_ENV=development`；生产校验和强制 MFA 依赖显式 production。Compose 已设 production，但独立 systemd/其他部署模板不能遗漏。
- Compose 已把控制面 HTTP、Admin 和 gRPC 默认绑定回环，PostgreSQL 无宿主机端口；不要为了外网访问把所有端口改为 `0.0.0.0`。远程工作站的 mTLS 入口需单独配置防火墙来源。
- 默认 Compose 没有服务内存/CPU/PID 配额；HTTP 并发和大小限制已存在，但仍应配置容器资源、网关速率和磁盘配额，并对审计、附件、软件包与备份容量报警。
- S3 备份允许显式 `http://`，数据库连接默认 `sslmode=disable`。同机 Docker 私网与跨公网不是相同的信任边界；远端数据库/备份应验证 TLS，SMB/SFTP/PVE/SSH 应留在受控网络。
- 核对真实运行镜像与本次源码一致、迁移已执行、三组生产密钥独立且离线备份、可信代理范围精确；本次没有验证线上状态。

## 验证结果及边界

- `go test -p 2 ./server/... ./workstation/...`：通过，其中多项使用已有缓存。数据库集成测试可能按自身条件跳过，本次未新增真实 PostgreSQL 验证。
- 临时 `TestAuditProbeResourceReferences`：无缓存执行通过，确认三种不安全操作确实被接受（这是复现成功，不是安全性通过）。
- 临时 `TestAuditProbePVECredentialRetarget`：无缓存执行通过，确认测试 Token 被外发，以及自定义管理角色不要求 MFA。仅使用内存存储、本地 TLS 测试服务器和合成凭据。
- server、workstation 目录分别运行 `go run golang.org/x/vuln/cmd/govulncheck@latest ./...`：均为 `No vulnerabilities found`。
- admin 目录 `npm audit --package-lock-only --json`：0 项漏洞。
- 临时探针执行后移除；业务代码未修复。依赖扫描不覆盖本报告中的业务逻辑越权，也未扫描部署镜像的系统软件包。

建议修复顺序：1/2/4 → 3/5/6 → 7/8/9；在修复前将管理控制面限制在受信任网络，并避免将共享工作站授权给互不信任的用户。
