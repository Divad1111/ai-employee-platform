# V2 总验收矩阵（M9 / T-1010）

> 对齐 `DEVELOPMENT_TASKS.md` §5.4。设计依据：§99、§63、§38、§70、§87、§43。

| 场景 | 期望 | 实测 | 覆盖 |
|------|------|------|------|
| Artifact 上传+SHA256 | 元数据完整，可下载 | PASS | `api.TestArtifactUploadDownloadHTTP` / `artifact` |
| Artifact 去重 | 同 hash 引用同一 storage | PASS | `artifact.TestPutDedupByHash` + HTTP `deduplicated` |
| 本地待上传队列 | 失败保留本地直至确认 | PASS | `artifactlocal` |
| Provider Registry CRUD | 按 OS/Arch 查询 | PASS | `registry` |
| Q-06 公钥下发 | `GET /api/providers/signing-key` | PASS | `api.TestSigningKeyPublic` |
| 签名/SHA 失败拒装 | 篡改包无法安装 | PASS | `registry` + `providers.Installer` + `updater.TestRejectBadPackage` |
| `aew agent install` | NOT_INSTALLED→INSTALLED + Event | PASS | `app.TestAgentInstallAndUpdateRollback` / `providers` |
| Update Health 失败回滚 | 回到 N-1 | PASS | `updater.TestInstallRollbackOnHealthFail` |
| Update 前 DRAINING + CLI rollback | BeginDrain；跨进程恢复 N-1 | PASS | `updater` + `app.TestAgentInstallAndUpdateRollback` |
| 资源超限排队 | 不分新 Job，原因可见 | PASS | `scheduler.TestResourceLimitKeepsQueued` |
| Prometheus metrics | `/metrics` 可刮取 | PASS | `api.TestMetricsEndpointScrapable` / `metrics.TestHandlerExposesRequiredSeries` |
| `doctor --fix` | 仅白名单；默认 dry-run | PASS | `diagnostics.TestDoctorFixWhitelist` / `app.TestDoctorFixDryRun` |
| Permission/Approval/TOTP | 见 SECURITY 矩阵 | PASS | M7 |
| Secret 无明文 | 见 SECURITY 矩阵 | PASS | M8 |

## Tracing

gRPC/Job OpenTelemetry span：**V2 延后**（指标已可刮取；完整 Tracing 可进后续迭代）。

## Q-06

**已决 B**：Provider 签名公钥由 **Control Plane 下发**（`/api/providers/signing-key`）；Workstation 安装时传入/缓存。开发环境 Registry 可自生成密钥对。
