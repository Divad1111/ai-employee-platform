-- +goose Up
-- VIEWER 不应持有系统级配置读权限：权限策略引擎 / 系统配置 / 飞书集成属运维能力
DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.permission_id = p.id
  AND r.name = 'VIEWER'
  AND p.code = 'system.read';

-- +goose Down
INSERT INTO role_permissions (role_id, permission_id, scope)
SELECT r.id, p.id, 'OWN'
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'VIEWER' AND p.code = 'system.read'
ON CONFLICT (role_id, permission_id) DO NOTHING;
