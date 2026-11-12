# AI Employee Platform 部署指南

完整步骤见 [安装与部署手册](../docs/DEPLOYMENT_GUIDE.md)，全部环境变量及 Windows/macOS/Linux/群晖密钥生成方法见 [.env 配置手册](../docs/ENV_CONFIGURATION.md)。安全加固前的已有部署先按 [旧数据与密钥迁移说明](../docs/SECURITY_HARDENING_2026-10-25.md#已有数据先迁移密钥不能直接替换变量) 升级，不要重新生成 `.env` 或直接替换加密密钥。

## 首次安装配置

部署目录需要 `docker-compose.yml`、`.env`，使用内置 HTTPS 网关时还需要 `Caddyfile`。模板中的镜像、域名、随机密码和三组独立密钥必须配置，不能只复制模板后按原样启动。

使用系统 Shell 生成新配置（macOS/Linux/群晖需 OpenSSL），无需 Python；替换为实际域名和已发布版本：

```bash
cd deploy
AIE_VERSION='vX.Y.Z'
sh ./generate-env.sh --domain employee.example.com \
  --server-image "ghcr.io/divad1111/ai-employee-platform/control-plane:${AIE_VERSION}" \
  --admin-image "ghcr.io/divad1111/ai-employee-platform/admin:${AIE_VERSION}"
```

Windows 使用内置 PowerShell 运行 `generate-env.ps1 -Domain ... -ServerImage ... -AdminImage ...`，无需 Python 或 OpenSSL，完整 PowerShell/CMD 命令见配置手册。两个原生脚本都不覆盖已有 `.env`，保留配置间空行；Shell 新文件权限为 0600，Windows 设置专用 ACL。数据库密码生产至少 20 字符；`AIE_SECRET_MASTER_KEY`、`AIE_BACKUP_MASTER_KEY`、`AIE_SETUP_TOKEN` 各至少 32 字符且不得复用。生成器统一生成独立的 64 字符随机文本。

## HTTPS 与远程工作站

`AIE_PUBLIC_SERVER_URL` 只声明客户端的 HTTPS 地址，不创建监听或签发证书；`AIE_HTTP_PORT` / `AIE_ADMIN_PORT` 是回环 HTTP 后端。

- **内置 Caddy**：配置 `AIE_DOMAIN`、`AIE_PUBLIC_SERVER_URL`，确认宿主机 80/443 可用，使用 `--profile public` 启动。公网域名需正确 DNS/证书验证条件；内网 IP 使用本地 CA，客户端需安装根证书，详见完整手册。
- **已有 HTTPS 代理**：不启动 gateway。将 `/api/*`、`/mcp` 转发至回环 API 端口，其余路径转发至回环 Admin 端口；保留 Host、正确覆盖转发头，并按真实代理来源配置 `AIE_TRUSTED_PROXIES`。不同宿主机上的代理不能直接访问这些回环端口。
- **远程工作站**：默认 `AIE_GRPC_BIND_IP=127.0.0.1` 只允许本机连接；改成服务器内网 IP，并限制防火墙允许的工作站来源。HTTPS gateway 不代理 gRPC。

例如以下是网络配置片段，镜像和密钥配置仍须保留：

```env
AIE_HTTP_PORT=9080
AIE_ADMIN_PORT=9088
AIE_GRPC_PORT=9090
AIE_GRPC_BIND_IP=192.168.4.243
AIE_ENV=production
AIE_SEED_DEMO=0
AIE_PUBLIC_SERVER_URL=https://192.168.4.243
AIE_DOMAIN=192.168.4.243
```

这里 HTTPS 仍是 **443**，不是 9080/9088。非默认 HTTPS 端口须同步修改实际网关监听与 `AIE_PUBLIC_SERVER_URL`。

## 启动与首次初始化

在线镜像部署，使用内置网关：

```bash
docker compose --profile public pull
docker compose --profile public up -d
docker compose --profile public ps -a
```

已有 HTTPS 代理时省略 `--profile public`。源码构建需将业务镜像引用设为本地构建标签，并使用覆盖文件：

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml --profile public up --build -d
```

群晖项目需保留同目录 Compose、私密 `.env` 与 `Caddyfile`；界面未启用 `public` profile 时普通“启动项目”不会启动网关，可通过 SSH 执行带 profile 的命令或使用已有代理。

通过实际 HTTPS 地址访问 `/setup`，输入 `AIE_SETUP_TOKEN` 创建管理员，再前往审批中心完成 TOTP 绑定。管理员随后可在工作站页生成默认 15 分钟、最长 60 分钟的一次性令牌，注册命令使用 HTTPS，不带已移除的 `--grpc-target` 参数。

## 离线镜像导入

Docker 可直接读取 gzip 镜像归档，无需先解压。服务器 Bash / SSH 终端中每个通配符只匹配一个文件时可直接执行：

```bash
docker load -i control-plane-dsm-*-amd64.tar.gz
docker load -i admin-dsm-*-amd64.tar.gz
```

ARM64 使用对应包；多版本目录填写完整文件名，或逐文件循环导入，不能将多个归档作为一次 `docker load -i` 的参数。PowerShell 使用实际完整文件名。

当前包内标签为 `aie-control-plane:latest`、`aie-admin:latest`，归档文件名版本不会自动成为镜像标签。核验 Release 的 `SHA256SUMS.txt`，导入后打版本标签，并让 `.env` 中业务镜像引用与实际标签一致。两份业务镜像不含 PostgreSQL、BusyBox、Caddy，完全离线时也需预先导入这些依赖。完整步骤见 [离线部署](../docs/DEPLOYMENT_GUIDE.md#方式-b下载-github-releases-离线镜像包无外网服务器--群晖-dsm-导入推荐)。

## 服务端口

| 服务 | 容器端口 | 默认宿主机绑定 | 用途 |
| --- | --- | --- | --- |
| HTTPS gateway（`public`） | 80 / 443 | 所有网卡 80 / 443 | HTTPS 入口；80 用于跳转/证书验证 |
| Admin Web | 80 | `127.0.0.1:8088` | 经 HTTPS 网关访问 |
| Control Plane HTTP | 8080 | `127.0.0.1:8080` | 网关转发 API/SSE/MCP |
| Control Plane gRPC | 9090 | `127.0.0.1:9090` | 显式配置内网绑定后允许工作站连接 |
| PostgreSQL | 5432 | 不发布 | 仅容器内网 |

## 更新与维护

升级前备份数据库、`data/server`、`.env` 和网关卷；保留数据库密码、CA、Secret/备份主密钥。在线部署将业务镜像改成核验后的版本/digest，再拉取重建；离线先导入、打版本标签、更新镜像引用，再重建，无需在线 `pull`。旧安全版本升级先完成上文链接的迁移，数据库自动迁移不会替你轮换密码和加密密钥。

```bash
# 内置网关在线部署；已有代理省略 --profile public
docker compose --profile public pull
docker compose --profile public up -d
docker compose logs --tail=100 migrate control-plane gateway

# 在 deploy 目录查看迁移状态和转储数据库
docker compose run --rm migrate -dir /migrations status
docker compose exec -T postgres sh -c 'pg_dumpall -c -U "$POSTGRES_USER"' > aie-db.sql
```

数据库转储不包含 Vault、CA、`.env`、备份主密钥或网关卷。冷备份需先停止写入，再保存实际数据目录和卷；Caddy 数据在 `gateway_data` / `gateway_config`，不要用 `down -v` 删除。
