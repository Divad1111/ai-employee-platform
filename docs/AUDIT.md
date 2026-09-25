# Audit 保留与分离策略（M8 / T-0911）

> 设计依据：§31、§86。Audit 与 Application Log **分离存储/查询**。

## 存储

| 通道 | 用途 | 是否含 Secret 明文 |
|------|------|-------------------|
| Application Log | 运行诊断 | **禁止** |
| Audit Store | 合规审计 | **禁止**（`value` 恒 `***`） |

V1/V2 开发态：`server/internal/audit.Memory`。生产应写入 PostgreSQL `audit_logs`（见 migrations）。

## 覆盖动作（§31 V2）

- `permission.decide`（ASK/DENY 必记；ALLOW 可采样）
- `approval.create` / `approval.approve` / `approval.reject` / `approval.totp`
- `secret.put` / `secret.access` / `secret.bind` / `secret.rotate` / `secret.delete`
- 既有 Login / Employee / Job / Workstation 等
- `quota.upsert` / `quota.delete`（角色预设只改不删；可删用户例外、用户额外、工作站与数字员工配额）
- `feishu.binding.upsert` / `feishu.binding.delete`
- `workstation.update` / `workstation.member.add` / `workstation.member.remove` / `workstation.cert.revoke`
- `workflow.upsert` / `workflow.delete` / `workflow.grant` / `workflow.revoke` / `workflow.import`
- `skill.upsert` / `skill.delete` / `skill.sync`
- `knowledge.upsert` / `knowledge.delete` / `knowledge.reindex`
- `mcp.token.issue` / `mcp.token.revoke`（不记录 Token 明文）
- `provider.upsert` / `provider.version.add`

## 查询与导出

- `GET /api/audit?prefix=secret.&limit=50`
- `GET /api/audit/export` → JSON 附件
- 过滤：`actor` / `action` / `prefix` / `target_type` / `target_id`

## 不可篡改

- 普通用户 **无** 删除 Audit API
- `POST /api/audit/archive` 仅 **SUPER_ADMIN**：按保留期归档（默认 90 天）
- 建议保留期：热数据 90 天；冷归档 ≥ 1 年（合规可调）

## 验收

见 [`TEST_MATRIX_V2_SECURITY.md`](./TEST_MATRIX_V2_SECURITY.md) M8 增补行。
