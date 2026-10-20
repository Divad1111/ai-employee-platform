# 本地附件功能部署记录

日期: 2026-10-20 (Asia/Shanghai).

这是附件功能首次部署的记录. 同日后续任务反馈修复的当前镜像和验证结果见 [飞书首次派单反馈排查](QA_FEISHU_RECEIPT_FEEDBACK.md#验证与本地部署).

已完成:

- 服务端、AEW 和共享包全量测试通过: `go test ./server/... ./workstation/... ./gen/... -timeout 180s`.
- 补充的飞书 SDK 集成测试通过: 引用原文件派单, 跨会话引用拒绝, 资源读取权限失败不创建任务并保留暂存.
- 已构建并重建中心服务和管理后台本地容器. PostgreSQL 容器及数据卷保留.
- 数据库第 32 号迁移执行成功, `jobs.input_attachments` 类型为 JSONB.
- `AIEmployeeWorkstation` Windows 服务已完成二进制替换和重启, 状态 Running. AEW 自检 `ready=true`, `reconnect=CONNECTED`.
- 中心服务 `/healthz` 返回 `ok`, 管理后台 HTTP 200. 部署中的附件下载接口对未签名请求返回 HTTP 401.
- 新构建、服务执行路径和本地 CLI 三份 AEW 二进制 SHA256 一致.

运行镜像:

```text
aie-control-plane:latest
sha256:28969bee8e28e803f9dfdc2783f4e05e4f4b31ad04880d67889106a55f26f074

aie-admin:latest
sha256:24e97a795266e5fb68dcd893bd8ce3f58882f6cf010588ef65e4b569dd54059f
```

AEW SHA256:

```text
C8C72B1A0BEC9E62715727D9333D266711F6CE1CD4F66BEAEF466A4D30AEDD74
```

服务执行路径: `workstation/bin/aew.exe`. 本地 CLI: `bin/aew.exe`. 构建文件: `bin/aew-new.exe`.

回滚副本保留在本地:

- `aie-control-plane:before-feishu-inputs-20261020`
- `aie-admin:before-feishu-inputs-20261020`
- `bin/aew-before-redeploy.exe`

AEW 更新脚本为 `scripts/redeploy-aew.ps1`, 执行前检查服务注册路径, 更新时校验散列, 失败尝试恢复旧二进制. 已通过 Windows UAC 执行完成; 结果为 `bin/aew-redeploy-result.json`.

本轮没有向真实飞书会话发送测试消息. 附件收取、下载和模型输入通过本机模拟飞书 API、真实 SDK 和协议测试验证; 飞书租户权限与实际机器人收发仍需在真实会话验收.
