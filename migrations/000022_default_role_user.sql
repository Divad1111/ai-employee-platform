-- +goose Up
-- 新增默认角色 USER（普通用户），赋予系统全部权限，且权限范围统一设为 OWN。

-- 1. 插入默认角色 USER
INSERT INTO roles (id, name, description) VALUES
    ('00000000-0000-0000-0000-000000000005', 'USER', '普通用户')
ON CONFLICT (name) DO UPDATE SET description = EXCLUDED.description;

-- 2. 将 permissions 表中的所有权限赋予 USER 角色，Scope 设为 OWN
INSERT INTO role_permissions (role_id, permission_id, scope)
SELECT r.id, p.id, 'OWN'
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'USER'
ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = 'OWN';

-- 3. 增加角色级月度配额预设（resource_type=ROLE, resource_id=USER）
INSERT INTO quota_policies (id, resource_type, resource_id, period_type, token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at)
VALUES
    (gen_random_uuid(), 'ROLE', 'USER', 'MONTHLY', 5000000, 0, 0, TRUE, NOW(), NOW())
ON CONFLICT (resource_type, resource_id, period_type) DO NOTHING;

-- +goose Down
DELETE FROM quota_policies WHERE resource_type = 'ROLE' AND resource_id = 'USER';
DELETE FROM role_permissions rp
USING roles r
WHERE rp.role_id = r.id AND r.name = 'USER';
DELETE FROM roles WHERE name = 'USER';
