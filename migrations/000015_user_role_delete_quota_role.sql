-- +goose Up
-- 用户删除 / 角色删除权限；角色级配额预设（ROLE）

INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000038', 'user.delete', '删除用户'),
    ('00000000-0000-0000-0001-000000000039', 'role.delete', '删除自定义角色'),
    ('00000000-0000-0000-0001-00000000003a', 'role.create', '创建自定义角色')
ON CONFLICT (code) DO NOTHING;

-- ADMIN 获得删除与创建角色权限
INSERT INTO role_permissions (role_id, permission_id, scope)
SELECT r.id, p.id, 'ALL'
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'ADMIN'
  AND p.code IN ('user.delete', 'role.delete', 'role.create')
ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = EXCLUDED.scope;

-- 角色级月度配额预设（resource_type=ROLE，resource_id=角色名）
-- 个人 USER 策略优先于角色预设；未配个人策略时按用户角色取对应 ROLE 限额
INSERT INTO quota_policies (id, resource_type, resource_id, period_type, token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at)
VALUES
    (gen_random_uuid(), 'ROLE', 'VIEWER', 'MONTHLY', 1000000, 0, 0, TRUE, NOW(), NOW()),
    (gen_random_uuid(), 'ROLE', 'OPERATOR', 'MONTHLY', 5000000, 0, 0, TRUE, NOW(), NOW()),
    (gen_random_uuid(), 'ROLE', 'ADMIN', 'MONTHLY', 50000000, 0, 0, TRUE, NOW(), NOW())
ON CONFLICT (resource_type, resource_id, period_type) DO NOTHING;

-- +goose Down
DELETE FROM quota_policies WHERE resource_type = 'ROLE' AND resource_id IN ('VIEWER', 'OPERATOR', 'ADMIN');
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id AND rp.permission_id = p.id
  AND p.code IN ('user.delete', 'role.delete', 'role.create');
DELETE FROM permissions WHERE code IN ('user.delete', 'role.delete', 'role.create');
