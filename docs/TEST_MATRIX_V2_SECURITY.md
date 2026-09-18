# V2 Security 验收矩阵（M7 子集）

> 对应任务 T-0907。设计依据：§28–§30、§88、§132。

| 场景 | 期望 | 实测 | 覆盖 |
|------|------|------|------|
| Git push 默认 ASK | 决策 ASK，建 Approval，Job→WAITING_APPROVAL | PASS | `approval` / `api/security_api_test` |
| Git push 批准后继续 | TOTP 通过 → APPROVAL_GRANTED → RUNNING | PASS | `approval.TestGitPushAskThenApproveWithTOTP` |
| Git push 拒绝 | REJECT → Job FAILED | PASS | `approval.TestRejectApprovalMarksJobFailed` |
| CRITICAL 无 TOTP | 直接 DENY，不可自动放行 | PASS | `approval.TestCriticalWithoutTOTPDeny` |
| 未配置规则 | 默认 DENY | PASS | `permission.TestDefaultDecide` |
| shutdown / 系统文件 | DENY | PASS | `permission` 默认规则 |
| Workstation Gate DENY | 阻断，无 shell 旁路 | PASS | `workstation/security` |
| Workstation Gate ASK | ErrNeedAsk，不上本地执行 | PASS | `workstation/security` |
| Admin 删 Employee 无 step-up | 403 | PASS | `api.TestStepUpRequiredForDelete` |
| step-up 后高风险操作 | 短时窗口内允许 | PASS | 同上 |
| TOTP 失败锁定 | Audit + 有限重试后锁定 | PASS | `approval.TestTOTPLockAfterFailures` |
| Secret 列表/详情无明文 | 仅 masked `***` | PASS | `api.TestSecretCRUDNoPlaintext` |
| Secret Access Audit | actor/secret_id/result，无明文 | PASS | `secret.TestManagerBindResolveAccessAudit` |
| Resolve 失败不回显 | Job/API 仅通用错误 | PASS | `api` resolve + manager |
| FileVault 持久化 | 重启可读 | PASS | `secret.TestFileVaultRoundTrip` |
| Rotate Credential | CRITICAL + TOTP | PASS | `api.TestRotateRequiresTOTP` |
| Audit 导出 | `GET /api/audit/export` 可下载 JSON | PASS | `api.TestAuditExportAndArchive` |
| Audit 归档 | 仅 SUPER_ADMIN；普通 ADMIN 403 | PASS | `api.TestAuditExportAndArchive` |

## API 速查

- `POST /api/permission/evaluate`
- `GET/POST /api/approvals…`
- `POST /api/auth/totp/enroll`、`POST /api/auth/step-up`
- `GET /api/permission/rules`（Workstation 同步缓存）
