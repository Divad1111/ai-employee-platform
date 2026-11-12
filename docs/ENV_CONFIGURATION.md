# .env 配置与密钥生成手册

本手册对应 [配置模板](../deploy/.env.example) 与当前 [Compose 配置](../deploy/docker-compose.yml)。完整部署流程见 [安装与部署手册](DEPLOYMENT_GUIDE.md)。适用于 Windows Docker Desktop（Linux 容器）、macOS、Linux 和群晖 DSM。

## 1. 文件使用方式

将配置保存为部署目录的 `.env`，与 `docker-compose.yml` 放在一起。从该目录执行 Compose，或显式指定 `--env-file`。每行使用 `KEY=value`，不要在行末添加续行反斜杠；空值写成 `KEY=`。`CHANGE_ME`、示例域名、镜像版本都必须替换成实际值。

当前终端中已设置的同名环境变量会优先于 `.env`；修改后没有生效时先检查是否存在覆盖项。更改环境变量或端口后使用 `docker compose up -d` 重建相关服务，单纯 `restart` 不会加载新配置。可用 `docker compose --profile public config --quiet` 检查语法，避免把包含秘密的完整配置输出到日志。

真实 `.env` 不应提交 Git、粘贴进聊天或公开工单。Linux/macOS/DSM 使用 `chmod 600 .env`；Windows 在文件属性的“安全”中限制 NTFS 读取权限，仅保留部署用户、必要管理员和系统账户。

**已有部署不要重新生成 `.env`，也不要直接替换数据库密码、Secret 主密钥或备份主密钥。** 丢失/替换主密钥会导致旧密文无法解密。安全加固前版本先按 [离线密钥迁移步骤](SECURITY_HARDENING_2026-10-25.md#已有数据先迁移密钥不能直接替换变量) 升级。

## 2. 首次安装：自动生成 .env（推荐）

首次配置使用系统原生脚本，**不需要 Python**：Windows 用 [generate-env.ps1](../deploy/generate-env.ps1)（PowerShell 5.1/7 + 内置 .NET），macOS/Linux/群晖用 [generate-env.sh](../deploy/generate-env.sh)（POSIX `sh` + OpenSSL + awk）。两个脚本都独立生成数据库密码与三组密钥，每项 **32 随机字节编码为 64 位十六进制文本**，保留模板注释和空行，不打印秘密，也不覆盖已有文件。

默认输出脚本所在目录的 `.env`；自定义输出分别使用 PowerShell 的 `-Output` 或 Shell 的 `--output`，相对路径以当前工作目录为准。Shell 新文件权限为 `0600`；Windows 创建文件时设置专用 ACL，仅当前用户、SYSTEM 和 Administrators 可访问。原 Python 脚本保留兼容旧调用，正常安装不再依赖它。

先确定实际镜像：在线部署填写已发布版本或核验后的 digest；源码构建可用 `aie-control-plane:local` / `aie-admin:local`；离线包填导入后对应的本地版本标签。下例使用源码构建标签，随后需构建相应镜像。

### Windows PowerShell

在仓库根目录执行，使用 Windows 自带 PowerShell 即可：

```powershell
Set-Location deploy
.\generate-env.ps1 -Domain employee.example.com -ServerImage aie-control-plane:local -AdminImage aie-admin:local
```

### Windows CMD

```cmd
cd deploy
powershell.exe -NoProfile -ExecutionPolicy Bypass -File .\generate-env.ps1 -Domain employee.example.com -ServerImage aie-control-plane:local -AdminImage aie-admin:local
```

PowerShell 如因执行策略无法直接运行脚本，也可使用上面的 CMD 命令；`Bypass` 仅对该次进程生效，不修改系统执行策略。

### macOS

在终端检查 `command -v openssl`，然后执行：

```bash
cd deploy
sh ./generate-env.sh --domain employee.example.com --server-image aie-control-plane:local --admin-image aie-admin:local
```

### Linux

需要系统的 `sh`、OpenSSL 和 awk；缺少 OpenSSL 时先安装发行版对应的 `openssl` 软件包：

```bash
cd deploy
sh ./generate-env.sh --domain employee.example.com --server-image aie-control-plane:local --admin-image aie-admin:local
```

### 群晖 DSM

SSH 进入部署目录，保存仓库中的 `generate-env.sh` 与 `.env.example`，使用 DSM 的系统 Shell，不要求 Bash 或 Python：

```sh
cd /volume1/docker/ai-employee
sh ./generate-env.sh --domain employee.example.com --server-image aie-control-plane:local --admin-image aie-admin:local
```

脚本会检查 OpenSSL 和 awk，缺失时明确报错且不生成配置。也可在其他机器运行原生脚本，再通过受控文件传输复制 `.env`。使用 `sh` 显式运行无需为脚本添加执行权限；从 Windows 复制 `.sh` 时保留 LF 换行，仓库已配置对应的 `.gitattributes`。

Shell 的 `--domain` / PowerShell 的 `-Domain` 填域名或 IPv4 地址，不含协议、端口和路径。例如 `--domain 192.168.4.243`（PowerShell 用 `-Domain 192.168.4.243`）会生成 `https://192.168.4.243`；仍须启动 HTTPS 网关并完成客户端证书信任。生成器不配置 DNS、监听或证书。

## 3. 各平台手动生成密码和三组密钥

先复制 `.env.example` 为 `.env`，再生成以下 **四个独立值** 填入对应项。生产要求数据库密码至少 20 字符，三组密钥各至少 32 字符；以下命令统一生成 64 字符随机文本。

| 配置项 | 用途 | 保存与更新要求 |
| --- | --- | --- |
| `POSTGRES_PASSWORD` | PostgreSQL 登录密码 | 已建库后需通过数据库命令改密，再同步配置；仅改 `.env` 不会改数据库密码 |
| `AIE_SECRET_MASTER_KEY` | Secret Vault、加密 TOTP 等敏感数据的主密钥 | 长期保存，不能直接替换变量轮换；恢复旧数据需要原值 |
| `AIE_BACKUP_MASTER_KEY` | 备份包和备份目标凭据的加密主密钥 | 长期保存；旧备份需对应旧密钥才能恢复 |
| `AIE_SETUP_TOKEN` | 首次 `/setup` 初始化口令 | 受控提供给初始化操作者；不是登录密码或工作站注册令牌，初始化后不能用于重置管理员 |

### Windows PowerShell 5.1 / 7（不需要 Python）

下面输出四行配置，每次循环都重新取随机字节，不能生成一次后复制到四个字段：

```powershell
$aieRng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
try {
    foreach ($aieKeyName in @('POSTGRES_PASSWORD', 'AIE_SECRET_MASTER_KEY', 'AIE_BACKUP_MASTER_KEY', 'AIE_SETUP_TOKEN')) {
        $aieBytes = New-Object byte[] 32
        $aieRng.GetBytes($aieBytes)
        $aieHex = [BitConverter]::ToString($aieBytes).Replace('-', '').ToLowerInvariant()
        Write-Output ($aieKeyName + '=' + $aieHex)
    }
} finally {
    $aieRng.Dispose()
}
```

### Windows CMD（调用内置 PowerShell）

```cmd
powershell.exe -NoProfile
```

进入 PowerShell 后粘贴上一节的原生随机数代码，会输出四行配置；无需安装 Python。需要直接生成完整 `.env` 时，使用第 2 节的 CMD 脚本命令即可。

### macOS（OpenSSL）

```bash
for aie_key_name in POSTGRES_PASSWORD AIE_SECRET_MASTER_KEY AIE_BACKUP_MASTER_KEY AIE_SETUP_TOKEN; do
  printf '%s=' "$aie_key_name"
  openssl rand -hex 32
done
```

### Linux（OpenSSL）

```bash
for aie_key_name in POSTGRES_PASSWORD AIE_SECRET_MASTER_KEY AIE_BACKUP_MASTER_KEY AIE_SETUP_TOKEN; do
  printf '%s=' "$aie_key_name"
  openssl rand -hex 32
done
```

### 群晖 DSM SSH（OpenSSL）

先用 `command -v openssl` 确认可用，再执行兼容 DSM `sh` 的命令：

```sh
for aie_key_name in POSTGRES_PASSWORD AIE_SECRET_MASTER_KEY AIE_BACKUP_MASTER_KEY AIE_SETUP_TOKEN; do
  printf '%s=' "$aie_key_name"
  openssl rand -hex 32
done
```

没有 OpenSSL 时先安装，或在其他机器运行原生生成脚本后安全复制 `.env`。手动生成的输出本身是秘密，只在可信本地终端使用。

## 4. 全部配置项说明

### 镜像与持久化

| 配置项 | 默认/模板值 | 是否必填及含义 |
| --- | --- | --- |
| `AIE_CONTROL_PLANE_IMAGE` | `aie-control-plane:security-20261025` | 必填；控制面与迁移共用镜像，填写实际存在的版本/digest。在线用完整仓库引用，离线用本地标签，不能按归档文件名猜测标签 |
| `AIE_ADMIN_IMAGE` | `aie-admin:security-20261025` | 必填；与控制面匹配的管理后台镜像 |
| `AIE_POSTGRES_IMAGE` | `postgres:16-alpine` | 可选；数据库镜像，不要未经数据库升级流程跨 PostgreSQL 主版本替换 |
| `AIE_INIT_IMAGE` | `busybox:1.37.0` | 可选；创建数据子目录并初始化容器用户权限的镜像 |
| `AIE_GATEWAY_IMAGE` | `caddy:2-alpine` | 可选；`public` profile 启动的 HTTPS 网关镜像 |
| `AIE_PG_DATA_DIR` | `./data/postgres` | 数据库宿主机目录，相对路径以 Compose 文件目录为基准。群晖可用 `/volume1/docker/ai-employee/data/postgres` |
| `AIE_SERVER_DATA_DIR` | `./data/server` | 包含 CA、Vault、附件、备份等；升级保留整个目录。群晖可用 `/volume1/docker/ai-employee/data/server` |

Caddy 证书和 CA 保存在命名卷 `gateway_data`，配置状态保存在 `gateway_config`，不在 `AIE_SERVER_DATA_DIR` 中，需另行备份。不要通过 `down -v` 删除它们。

### 数据库与密钥

| 配置项 | 默认/模板值 | 是否必填及含义 |
| --- | --- | --- |
| `POSTGRES_USER` | `aie` | 内置数据库初次创建的用户名，修改变量不会重命名已存在用户 |
| `POSTGRES_PASSWORD` | `CHANGE_ME...` | 必填；独立随机密码，生成方式见上文 |
| `POSTGRES_DB` | `aie` | 初次创建的数据库名，修改变量不会重命名已存在数据库 |
| `AIE_DATABASE_URL` | 空 | 可选；非空时覆盖控制面/迁移的数据库连接。默认留空，按 `POSTGRES_*` 连接容器 `postgres:5432`，避免 URI 密码转义问题。手写 URI 时特殊字符需 URL 编码，外部数据库应配置合适的 TLS；内置 PostgreSQL 容器仍会启动 |
| `AIE_SECRET_MASTER_KEY` | `CHANGE_ME...` | 生产必填；Secret/TOTP 主密钥，生成和保存要求见上文 |
| `AIE_BACKUP_MASTER_KEY` | `CHANGE_ME...` | 生产必填；备份主密钥，生成和保存要求见上文 |
| `AIE_BACKUP_PREVIOUS_KEY` | 空 | 首次部署留空；需要时填实际上一把备份主密钥，辅助解密旧密文，不能随机生成，也不替代正式迁移 |
| `AIE_SETUP_TOKEN` | `CHANGE_ME...` | 生产必填；首次初始化口令，生成方式见上文 |

不要继续使用旧 `AIE_MASTER_KEY`，生产校验会拒绝它。迁移工具的 `AIE_OLD_SECRET_MASTER_KEY` / `AIE_OLD_BACKUP_MASTER_KEY` 是维护窗口专用输入，不是正常部署要新生成的字段，按安全加固文档操作。

### 网络与运行模式

| 配置项 | 默认/模板值 | 是否必填及含义 |
| --- | --- | --- |
| `AIE_HTTP_PORT` | `8080` | 宿主机 `127.0.0.1` 的 HTTP API 端口，映射容器 8080；改为 9080 不会启用 HTTPS 或远程访问 |
| `AIE_ADMIN_PORT` | `8088` | 宿主机 `127.0.0.1` 的 HTTP 管理后台端口，映射容器 80；改为 9088 仍是 HTTP，经 HTTPS 代理访问 |
| `AIE_GRPC_PORT` | `9090` | 宿主机 mTLS gRPC 端口，映射容器 9090，并通过注册端点告知工作站；NAT 对外端口应与声明一致 |
| `AIE_GRPC_BIND_IP` | `127.0.0.1` | 远程工作站需要改成服务器内网 IP，并限制防火墙来源。`0.0.0.0` 表示所有 IPv4 网卡，需明确评估暴露范围 |
| `AIE_ENV` | `production` | 生产保持该值，启用强密钥/HTTPS 配置校验和管理员 MFA 要求，不要改成 development 绕过错误 |
| `AIE_SEED_DEMO` | `0` | 演示业务数据开关，生产保持 0，不用于生成生产管理员 |
| `AIE_PUBLIC_SERVER_URL` | `https://CHANGE_ME.example.com` | 生产必填；实际完整 HTTPS 基址，非默认 HTTPS 端口需带端口；不自动创建监听、证书或代理 |
| `AIE_DOMAIN` | `CHANGE_ME.example.com` | 内置 Caddy 的域名或 IP，不含协议/路径；当前 Compose 发布宿主机 80/443。只设置该值不会启用 `public` profile；其他代理需在代理自身配置域名/证书 |

例如 `AIE_HTTP_PORT=9080`、`AIE_ADMIN_PORT=9088`、`AIE_PUBLIC_SERVER_URL=https://192.168.4.243` 表示两个回环 HTTP 后端，对外 HTTPS 仍是 443。远程注册不能改成 HTTP 9080，客户端仅允许字面回环 IP 使用 HTTP。HTTPS 网关与 gRPC 是两条连接，网关不替代 9090 的监听和放行。

### 代理与软件源信任

| 配置项 | 默认/模板值 | 是否必填及含义 |
| --- | --- | --- |
| `AIE_TRUSTED_PROXIES` | `172.30.20.10/32` | 逗号分隔的可信代理 IP/CIDR，默认对应 gateway 的固定 IP。换代理/Docker 子网时同步修改，按服务实际收到的代理源地址填写，不是工作站地址。禁止 `0.0.0.0/0` / `::/0` |
| `AIE_SOFTWARE_ALLOWED_HOSTS` | GitHub API、下载与重定向主机列表 | 逗号分隔的精确 HTTPS 主机名，不含协议/路径，不支持通配符；生产软件源仅允许默认 443。私有源需加入源主机和重定向目标，白名单不能替代签名 |
| `AIE_SOFTWARE_SIGNING_KEY` | 空 | 可选；生产自动接收远程 release 时必需。填匹配发布方的原始 Ed25519 公钥 Base64；留空不阻止基础服务启动，但相关远程导入会被拒绝 |

容器内 `AIE_HTTP_ADDR=:8080`、`AIE_GRPC_ADDR=:9090` 和 CA/Vault/附件路径由 Compose 固定设置，仅在 `.env` 添加同名变量不会改变它们。修改时需同步调整 Compose、路由与挂载；普通部署使用上表宿主机端口/持久化目录即可。

## 5. 软件发布签名公钥（可选）

`AIE_SOFTWARE_SIGNING_KEY` 必须是 **Ed25519 原始 32 字节公钥的标准 Base64**，通常 44 字符。PEM 文本、完整 DER 的 Base64、私钥、随机口令或 SHA256 都不能代替。对接已有发布源，应通过可信渠道取得发布方公钥；自己另生成一对密钥无法验证别人已有的签名。

自己维护发布源时，在**发布机**生成密钥对，私钥仅用于发布签名，平台只配公钥。以下跨平台示例需要 Python 3 与 `cryptography`，基础平台部署无需该依赖。

在私密工作目录保存为 `generate-publisher-key.py`：

```python
import base64
import os
from pathlib import Path
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey

private_path = Path('aie-release-private.pem')
public_path = Path('aie-release-public.b64')
if private_path.exists() or public_path.exists():
    raise SystemExit('Key files already exist; refusing to overwrite')
key = Ed25519PrivateKey.generate()
private_pem = key.private_bytes(
    serialization.Encoding.PEM,
    serialization.PrivateFormat.PKCS8,
    serialization.NoEncryption(),
)
public_raw = key.public_key().public_bytes(
    serialization.Encoding.Raw,
    serialization.PublicFormat.Raw,
)
with os.fdopen(os.open(private_path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'wb') as f:
    f.write(private_pem)
with public_path.open('x', encoding='ascii') as f:
    f.write(base64.b64encode(public_raw).decode('ascii') + '\n')
print('Created key files; keep the private key on the publisher')
```

**Windows PowerShell / CMD**（没有 `py` 时使用 `python`）：

```cmd
py -3 -m venv .venv-signing
.venv-signing\Scripts\python.exe -m pip install cryptography
.venv-signing\Scripts\python.exe generate-publisher-key.py
```

Windows 的 `0o600` 不能替代 ACL，需限制私钥文件的 NTFS 读取权限。

**macOS**：

```bash
python3 -m venv .venv-signing
.venv-signing/bin/python -m pip install cryptography
.venv-signing/bin/python generate-publisher-key.py
chmod 600 aie-release-private.pem
```

**Linux**：

```bash
python3 -m venv .venv-signing
.venv-signing/bin/python -m pip install cryptography
.venv-signing/bin/python generate-publisher-key.py
chmod 600 aie-release-private.pem
```

**群晖 DSM**：推荐在上述 Windows/macOS/Linux 发布机运行示例，仅把公钥文件的单行内容复制到 NAS 的 `.env`。若 NAS 本身就是发布机，具备 Python 3、venv 和兼容的 cryptography 安装条件时可使用相同 Linux 命令；否则不需要在 NAS 安装此依赖。

将 `aie-release-public.b64` 内容填入 `AIE_SOFTWARE_SIGNING_KEY=`。发布程序用对应私钥对服务端定义的 `ReleaseSigningPayload` 字节签名，Base64 编码后填入 release 的 `signature`。只配置公钥不会自动为已有 release 签名；载荷格式见 [软件发布信任](SECURITY_HARDENING_2026-10-25.md#软件发布信任)。换公钥应与发布流程同步更新。

## 6. 不需要在 .env 手填的身份材料

- **HTTPS 证书**由 Caddy/已有代理管理。内网 IP 使用本地 CA 时，所有客户端需安装根证书，操作见部署手册；浏览器忽略证书错误不等于 `aew` 已信任。
- **工作站 CA**由服务端在持久化 CA 目录维护，需随 `data/server` 备份；与 Secret 主密钥、HTTPS 网关 CA 不同。
- **工作站私钥**由 `aew register` 在本机生成，提交 CSR 后取得客户端证书，私钥不上传。
- **Enrollment Token**由完成 TOTP 的管理员在“工作站节点”生成，默认 15 分钟、最长 60 分钟、单次使用，不能用初始化口令或主密钥代替。

外部参考：[Compose 环境变量优先级](https://docs.docker.com/compose/how-tos/environment-variables/envvars-precedence/)、[Caddy 本地 HTTPS 与信任](https://caddyserver.com/docs/running#local-https-with-docker)、[Ed25519 生成与序列化](https://cryptography.io/en/latest/hazmat/primitives/asymmetric/ed25519/)。
