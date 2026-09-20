# AI Employee Platform 部署指南

本目录支持两种部署方式：
1. **基于镜像直接部署（生产环境 / 群晖 DSM 推荐）**：无需任何 Go / Node 构建环境，直接拉取官方发布的轻量多架构镜像（适配 x86_64 及 ARM64）。
2. **基于源码本地构建部署（开发调试）**：在本地源码环境下直接编译容器镜像并启动。

---

## 一、生产 / 群晖 DSM 容器镜像部署（推荐）

### 1.1 特性保障
- **数据持久化与防丢失**：全量数据（PostgreSQL 数据库集群、CA 根证书私钥、Secret 密钥）均自动映射落盘至宿主机本地目录 `./data/`（或自定义路径如 `/volume1/docker/ai-employee/data`）。
- **零配置平滑更新**：更新镜像时（`docker compose pull && docker compose up -d`），原数据库与 CA 证书私钥数据完全保留；内置 `migrate` 容器自动执行增量数据库表结构同步，**无需重新配置数据库与平台管理员**。

### 1.2 启动步骤（Linux / 宿主机）
```bash
cd deploy

# 1. 拷贝并按需调整环境配置（可选修改端口或目录路径）
cp .env.example .env

# 2. 拉取最新镜像并后台启动全套服务
docker compose pull
docker compose up -d
```

### 1.3 群晖 DSM (Container Manager) 一键部署
1. 打开群晖 DSM，进入 **Container Manager**。
2. 点击左侧 **「项目 (Project)」** -> **「新增」**。
3. 填入项目名称（例如 `ai-employee`），路径选择 NAS 共享目录（例如 `/volume1/docker/ai-employee`）。
4. 来源选择 **「创建 docker-compose.yml」**，将本目录下的 `docker-compose.yml` 内容粘贴进去。
5. （可选）在同目录下创建或上传 `.env` 文件，指定：
   ```env
   AIE_PG_DATA_DIR=/volume1/docker/ai-employee/data/postgres
   AIE_SERVER_DATA_DIR=/volume1/docker/ai-employee/data/server
   ```
6. 点击下一步完成，群晖将自动拉取双架构镜像并持久化启动。
7. 后续升级：在项目列表中右键点击该项目 -> **「操作」** -> **「拉取最新映像」** 并重启，所有业务数据无损保留，自动完成数据库增量迁移。

---

## 二、本地源码编译部署（开发模式）

若希望直接基于当前代码仓库的最新修改构建镜像并运行：

```bash
cd deploy
docker compose -f docker-compose.yml -f docker-compose.build.yml up --build -d
```

停止服务：
```bash
docker compose down
```
*(注：`docker compose down` 仅停止并删除容器，本地 `./data/` 目录中的数据库与平台数据依然完整保留！)*

---

## 三、服务端口与访问

| 服务 | 容器内端口 | 宿主机默认端口 | 用途说明 |
|------|-----------|----------------|----------|
| **Admin Web 控制台** | 80 | `http://localhost:8088` | 管理后台界面（首次访问自动进入 `/setup` 向导） |
| **Control Plane HTTP** | 8080 | `http://localhost:8080` | REST API 与 SSE 事件推送 |
| **Control Plane gRPC** | 9090 | `localhost:9090` | 计算工作站 (aew) mTLS 长连接接口 |
| **PostgreSQL 数据库** | 5432 | `localhost:5432` | 平台主数据库持久化存储 |

---

## 四、常见维护操作

### 4.1 手动查看数据库迁移状态
```bash
cd deploy
docker compose run --rm migrate -dir /migrations status
```

### 4.2 备份与还原数据
由于所有数据已映射到宿主机本地 `./data/`：
- **热备份**：可以直接使用 `docker exec -t aie-postgres pg_dumpall -c -U aie > backup.sql`
- **冷备份**：在停止容器状态下，直接打包备份宿主机上的 `./data` 目录即可。
