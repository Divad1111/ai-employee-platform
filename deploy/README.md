# 部署

## 启动（需 Docker Desktop 运行中）

```bash
cd deploy
docker compose up -d postgres
# 等待 healthy 后：
cd ../server
go run ./cmd/migrate -dir ../migrations up

# 可选：整套
cd ../deploy
docker compose up --build
```

## 服务

| 服务 | 地址 |
|------|------|
| Postgres | localhost:5432 |
| Control Plane HTTP | localhost:8080 |
| Control Plane gRPC | localhost:9090 |
| Admin | localhost:8088 |

V1 **不包含 Redis**。
