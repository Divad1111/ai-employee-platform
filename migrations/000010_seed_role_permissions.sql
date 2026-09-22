-- +goose Up
-- 补齐权限码与角色赋权。
-- 背景：000001 只插入了 permissions，从未写入 role_permissions；
-- 此前依赖 ListPermissions 在「零行」时走 fallbackPermissions。
-- 000009 给 ADMIN 写入 4 条 workflow.* 后，fallback 不再触发，导致除工作流外全站 403。

INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000001', 'employee.read', '读取 Employee'),
    ('00000000-0000-0000-0001-000000000002', 'employee.write', '写入 Employee'),
    ('00000000-0000-0000-0001-000000000003', 'employee.delete', '删除 Employee'),
    ('00000000-0000-0000-0001-000000000004', 'workstation.read', '读取 Workstation'),
    ('00000000-0000-0000-0001-000000000005', 'workstation.write', '写入 Workstation'),
    ('00000000-0000-0000-0001-000000000006', 'job.read', '读取 Job'),
    ('00000000-0000-0000-0001-000000000007', 'job.cancel', '取消 Job'),
    ('00000000-0000-0000-0001-000000000008', 'approval.read', '读取审批'),
    ('00000000-0000-0000-0001-000000000009', 'approval.approve', '批准审批'),
    ('00000000-0000-0000-0001-00000000000a', 'secret.read', '读取 Secret 元数据'),
    ('00000000-0000-0000-0001-00000000000b', 'secret.write', '写入 Secret'),
    ('00000000-0000-0000-0001-00000000000c', 'system.read', '读取系统配置'),
    ('00000000-0000-0000-0001-00000000000d', 'system.write', '写入系统配置'),
    ('00000000-0000-0000-0001-00000000000e', 'workspace.read', '读取工作区'),
    ('00000000-0000-0000-0001-00000000000f', 'workspace.write', '写入工作区'),
    ('00000000-0000-0000-0001-000000000010', 'session.read', '读取会话'),
    ('00000000-0000-0000-0001-000000000011', 'session.write', '写入会话'),
    ('00000000-0000-0000-0001-000000000012', 'job.write', '创建/推进 Job'),
    ('00000000-0000-0000-0001-000000000013', 'message.read', '读取消息'),
    ('00000000-0000-0000-0001-000000000014', 'message.write', '发送消息'),
    ('00000000-0000-0000-0001-000000000015', 'audit.read', '读取审计'),
    ('00000000-0000-0000-0001-000000000016', 'enrollment.write', '签发工作站接入令牌'),
    ('00000000-0000-0000-0001-000000000020', 'workflow.read', '读取工作流/技能/知识'),
    ('00000000-0000-0000-0001-000000000021', 'workflow.write', '写入工作流/技能/知识'),
    ('00000000-0000-0000-0001-000000000022', 'workflow.delete', '删除工作流/技能/知识'),
    ('00000000-0000-0000-0001-000000000023', 'workflow.grant', '向员工授权工作流并签发 MCP Token')
ON CONFLICT (code) DO NOTHING;

-- ADMIN：全量业务权限（对齐 fallbackPermissions）
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'ADMIN'
  AND p.code IN (
    'employee.read', 'employee.write', 'employee.delete',
    'workstation.read', 'workstation.write',
    'workspace.read', 'workspace.write',
    'session.read', 'session.write',
    'job.read', 'job.write', 'job.cancel',
    'message.read', 'message.write',
    'audit.read',
    'approval.read', 'approval.approve',
    'secret.read', 'secret.write',
    'system.read', 'system.write',
    'enrollment.write',
    'workflow.read', 'workflow.write', 'workflow.delete', 'workflow.grant'
  )
ON CONFLICT DO NOTHING;

-- OPERATOR
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'OPERATOR'
  AND p.code IN (
    'employee.read', 'workstation.read', 'workspace.read',
    'session.read', 'job.read', 'job.write', 'job.cancel',
    'message.read', 'message.write', 'approval.read',
    'workflow.read', 'workflow.grant'
  )
ON CONFLICT DO NOTHING;

-- VIEWER
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'VIEWER'
  AND p.code IN (
    'employee.read', 'workstation.read', 'workspace.read',
    'session.read', 'job.read', 'message.read',
    'approval.read', 'system.read', 'audit.read',
    'workflow.read'
  )
ON CONFLICT DO NOTHING;

-- +goose Down
-- 不回滚权限赋权，避免再次触发 fallback 空洞
SELECT 1;
