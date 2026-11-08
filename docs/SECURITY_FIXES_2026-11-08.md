# 外网部署安全修复与回归测试

本次落实 [独立审查报告](SECURITY_EXTERNAL_REVIEW_2026-11-08.md) 的九项问题。修复针对源码和仓库部署模板；需要重新构建并升级控制面、Admin 和工作站才能生效。

## 修复结果

| 原编号 | 实现 | 新增/扩展验证 |
| --- | --- | --- |
| 1 | 员工、工作区修改校验目标节点；员工创建限制所有者；工作区引用同时检查节点与已有绑定。任务/会话创建检查资源一致性；调度出队再次读取当前账号、角色和节点授权。 | `TestResourceReferenceAuthorization`、`TestDispatchRechecksCurrentAuthorization`、`TestDeniedOrInconsistentAssignmentSendsNoCommand`。覆盖跨节点、跨用户引用、拒绝请求无副作用、权限撤销及不下发命令。 |
| 2 | 工作区绑定、解绑、删除检查被修改的员工，以及员工原工作区的写权限。 | 本人工作区不能绑定受害者、创建时不能注入受害者 ID；合法本人绑定仍成功。 |
| 3 | 工作站仅接受员工专属目录，拒绝越界、非法 ID、相对路径与内部符号链接；目录创建使用 `os.Root` 限制路径逃逸；会话启动前复核。Agent/模型探测子进程使用环境白名单，保留显式注入变量。生产调度禁止同一工作站混用不同所有者的员工。 | `TestWorkspaceIsolation`、`TestOSRunnerFiltersInheritedEnvironment`、`TestBaseDoesNotInheritSecretsOrLoaderOverrides`、`TestProductionWorkstationCannotMixTenants`。 |
| 4 | PVE、SSH 主机和软件源配置额外要求 `infra.credential:ALL`，不能凭 `infra.manage` 修改凭据发送目标。目标配置与明文查看采用同一凭据权限边界。 | `TestInfrastructureManagerCannotRetargetStoredCredential`：无权限者修改/新建均 403，目标及密钥不变；明确授权后合法同步仍可用。 |
| 5 | SSH 票据绑定当前 Web 会话；消费前和握手后重新授权；活跃连接每秒复核，注销、禁用、改密码、角色撤销后断开。 | 扩展 `TestInfraConsolePTYUsesOneUseTicket`，使用真实本地 SSH/WebSocket，覆盖一次性票据、四类撤权、已连接终端及未消费票据。 |
| 6 | 按有效管理权限判断 MFA，自定义角色、全局写权限及通配授权均不能绕过；REST、MCP 和禁用 TOTP 的检查统一。 | `TestCustomManagementRolesRequireMFA`，覆盖多个自定义权限、真实会话拒绝与 MFA 验证后允许。 |
| 7 | 所有密码复核加入账号限流；step-up/TOTP 操作同时限制 IP 和用户请求数；限流返回 429，保留现有会话。 | `TestReauthenticationIsRateLimitedWithoutInvalidatingSession`、`TestStepUpEndpointRateLimits`。 |
| 8 | 浏览器改用 HttpOnly、SameSite=Strict Cookie；生产强制 Secure；JSON 不返回浏览器会话密钥，前端清理旧 Bearer 存储，只保留非秘密会话标记。Cookie 写操作要求自定义请求头，并校验 Origin/Fetch Metadata；CLI Bearer 保持可用。Caddy/Nginx 强制 CSP，兼容本地 Monaco Worker。 | `TestBrowserCookieSessionAndCSRF`；真实浏览器测试登录、刷新、JS 不可读 Cookie、退出、CSP 阻止内联脚本；技能编辑和备份页面在部署 CSP 下运行。 |
| 9 | CI 在两个 Go 模块目录分别扫描；Admin CI 增加构建产物的浏览器安全回归。 | 本地按同样模块目录运行扫描；浏览器脚本纳入 CI。 |

额外加固：原生控制面默认 production，缺失生产密钥即拒绝启动；远端数据库要求验证证书的 TLS，生产 S3 拒绝 HTTP；Compose 增加可配置的 CPU/内存及 PID 上限。

## 升级与兼容性变化

1. **升级后重新登录。** 前端清除旧 `aie_admin_token`；浏览器改用 Cookie。生产管理页必须通过 HTTPS 访问。自定义前端的 Cookie 写请求需要 `X-AIE-Browser: 1`，且同源发送；跨站前端不在默认支持范围。CLI/API 客户端仍可以使用 Authorization Bearer。
2. **迁移工作区路径。** 默认允许目录为 `WorkspaceRoot/<员工ID>/...`。例如使用 `AIE_DATA_DIR=/home/aie/.aie` 时，员工 `EMP-xxx` 的项目应在 `/home/aie/.aie/employees/EMP-xxx/project`。先停止该员工任务，备份并迁移项目，再修改后台路径。旧路径超出根目录时会拒绝执行；升级不会自动移动或删除项目。
3. **按用户隔离执行账号。** production 下，一个工作站只运行其创建者名下的员工，公共角色共享不再允许跨所有者派发。不同用户应使用独立 OS 账号或 VM，并分别注册工作站；历史节点须核实创建者与员工所有者。路径和环境白名单不构成完整 OS 沙箱，也不能保护用户主动挂入执行账号的其他私有文件。
4. **补齐权限及 MFA。** 负责修改 PVE/SSH/软件源配置的角色需显式授予 `infra.credential:ALL`；其他运维角色可保留只读、同步或已配置目标的部署权限。自定义管理角色，以及拥有密钥、节点注册或用户管理等敏感权限的账号，需要绑定 MFA。
5. **运行环境。** 本地开发显式设 `AIE_ENV=development`；生产远程 PostgreSQL 使用 `sslmode=verify-full` 并配置可信 CA，S3 使用 HTTPS。Agent 不再继承任意宿主机环境密钥，应使用受控密钥注入；HOME/PATH 等基本运行环境仍保留。
6. **容量与入口。** Compose 默认给控制面 2 CPU/2 GiB、数据库 2 CPU/1 GiB、Admin 1 CPU/128 MiB、网关 1 CPU/256 MiB；可用 `AIE_<服务名>_CPUS`、`AIE_<服务名>_MEMORY` 调整。仍需按真实流量配置防火墙、访问代理、磁盘配额和容量告警。应用代码不能替代宿主机与云网络策略。

## 验证命令

```sh
go test -p 2 ./server/... ./workstation/...
go test -race ./server/internal/api ./server/internal/auth ./server/internal/infrastructure ./server/internal/scheduler ./workstation/internal/runtime ./workstation/internal/runtime/process ./workstation/internal/runtimeenv
cd server && govulncheck ./...
cd ../workstation && govulncheck ./...
cd ../admin && npm run build
node tests/security.browser.cjs
ASSET_UI_START_SERVER=1 ASSET_UI_PREVIEW=1 node tests/skillAssets.browser.cjs
BACKUP_UI_START_SERVER=1 node tests/backups.browser.cjs
```

浏览器测试需要 Playwright。CI 使用固定版本的独立临时安装，不修改应用依赖锁文件；本地可通过 `PLAYWRIGHT_MODULE_PATH` 指定安装位置。默认使用本机 Chrome，CI 通过 `PLAYWRIGHT_CHANNEL=chromium` 使用安装的 Chromium。

本次验证未连接线上 PVE、飞书、SSH 或公网入口；终端测试仅连接本地合成 SSH 服务。数据库模式未改变，现有数据库集成测试仍按各自配置决定是否运行。

## 本地执行结果（2026-11-08）

- 完整 `go test -p 2 ./server/... ./workstation/...` 通过；新增关键安全探针已改为持久化的拒绝/允许回归测试。
- 上述 7 个敏感包的完整 `-race` 检查通过。过程中发现旧 SSE 测试并发读写 `httptest.ResponseRecorder` 的竞态，已改成真实 HTTP 流测试，并复测通过。
- Admin 生产构建通过；Cookie/CSP 安全流程、技能资产编辑完整流程、备份页面三组浏览器测试通过。技能编辑用例同时修正了 macOS 选择全部快捷键及 Monaco 模型切换的同步等待。
- server/workstation 分别执行 govulncheck，均无漏洞告警；npm audit 为 0。
- CGO 关闭的 Linux amd64 服务端构建通过；Compose public profile 渲染通过，确认 PostgreSQL 无发布端口、HTTP/Admin/gRPC 默认回环及各常驻服务资源上限；Nginx 容器配置检查通过。
- 前端构建仅保留已有的大产物体积提示。未在本次验证中连接生产环境或更新线上镜像。
