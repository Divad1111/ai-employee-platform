# Proto 目录

本目录是 Control Plane ↔ Workstation 协议的**唯一来源**。  
禁止在 Go 业务代码中手写与此重复的通信结构。

## 生成

```bash
# 需已安装：buf、protoc-gen-go、protoc-gen-go-grpc（见 PATH）
make proto
# 或
cd proto && buf generate && buf lint
```

产物：`../gen/go/aie/v1/*.pb.go`

## 文件

| 文件 | 说明 |
|------|------|
| `aie/v1/common.proto` | EnvelopeMeta、防重放字段 |
| `aie/v1/workstation.proto` | Workstation 状态/能力 |
| `aie/v1/heartbeat.proto` | 心跳 |
| `aie/v1/command.proto` | Server→Worker 命令与 ACK |
| `aie/v1/event.proto` | Worker→Server 事件 |
| `aie/v1/job.proto` | Job 状态与上下文 |
| `aie/v1/session.proto` | Session 状态 |
| `aie/v1/worker_service.proto` | gRPC 双向流服务 |
