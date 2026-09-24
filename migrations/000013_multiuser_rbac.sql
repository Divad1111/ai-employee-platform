-- +goose Up
-- 多用户 RBAC / Scope / workstation_users / Employee owner / Quota
-- 依据：AI_Employee_System_MultiUser_RBAC_Design.md §11.1 §11.3 §12–13 §36–39

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ---------- users ----------
ALTER TABLE users ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'active';
ALTER TABLE users ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_login_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- ---------- role_permissions.scope ----------
ALTER TABLE role_permissions ADD COLUMN IF NOT EXISTS scope TEXT NOT NULL DEFAULT 'NONE';

-- 已有赋权：ADMIN → ALL；OPERATOR/VIEWER → OWN
UPDATE role_permissions rp
SET scope = 'ALL'
FROM roles r
WHERE rp.role_id = r.id AND r.name IN ('ADMIN', 'SUPER_ADMIN');

UPDATE role_permissions rp
SET scope = 'OWN'
FROM roles r
WHERE rp.role_id = r.id
  AND r.name IN ('OPERATOR', 'VIEWER')
  AND rp.scope = 'NONE';

-- 新权限码
INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000030', 'user.read', '读取用户'),
    ('00000000-0000-0000-0001-000000000031', 'user.create', '创建用户'),
    ('00000000-0000-0000-0001-000000000032', 'user.update', '更新用户'),
    ('00000000-0000-0000-0001-000000000033', 'user.disable', '禁用/启用用户'),
    ('00000000-0000-0000-0001-000000000034', 'role.read', '读取角色权限'),
    ('00000000-0000-0000-0001-000000000035', 'role.update', '更新角色权限'),
    ('00000000-0000-0000-0001-000000000036', 'quota.read', '读取配额'),
    ('00000000-0000-0000-0001-000000000037', 'quota.update', '更新配额')
ON CONFLICT (code) DO NOTHING;

-- ADMIN 获得用户/角色/配额管理（ALL）
INSERT INTO role_permissions (role_id, permission_id, scope)
SELECT r.id, p.id, 'ALL'
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'ADMIN'
  AND p.code IN (
    'user.read', 'user.create', 'user.update', 'user.disable',
    'role.read', 'role.update',
    'quota.read', 'quota.update'
  )
ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = EXCLUDED.scope;

-- OPERATOR：quota.read OWN；无用户管理
INSERT INTO role_permissions (role_id, permission_id, scope)
SELECT r.id, p.id, 'OWN'
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'OPERATOR'
  AND p.code IN ('quota.read')
ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = EXCLUDED.scope;

-- ---------- workstations：创建者（非访问依据，§11.3）----------
ALTER TABLE workstations ADD COLUMN IF NOT EXISTS created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL;

-- ---------- workstation_users（§11.1）----------
CREATE TABLE IF NOT EXISTS workstation_users (
    id              UUID PRIMARY KEY,
    workstation_id  TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role            TEXT NOT NULL DEFAULT 'MEMBER',
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_ws_users_user ON workstation_users(user_id);
CREATE INDEX IF NOT EXISTS idx_ws_users_ws ON workstation_users(workstation_id);

-- ---------- employees.owner_user_id ----------
ALTER TABLE employees ADD COLUMN IF NOT EXISTS owner_user_id UUID REFERENCES users(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_employees_owner ON employees(owner_user_id);

-- ---------- Quota（§12–13）----------
CREATE TABLE IF NOT EXISTS quota_policies (
    id                  UUID PRIMARY KEY,
    resource_type       TEXT NOT NULL,
    resource_id         TEXT NOT NULL,
    period_type         TEXT NOT NULL DEFAULT 'MONTHLY',
    token_limit         BIGINT NOT NULL DEFAULT 0,
    request_limit       BIGINT NOT NULL DEFAULT 0,
    concurrency_limit   INT NOT NULL DEFAULT 0,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (resource_type, resource_id, period_type)
);

CREATE TABLE IF NOT EXISTS workstation_user_quotas (
    id                  UUID PRIMARY KEY,
    workstation_id      TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    period_type         TEXT NOT NULL DEFAULT 'MONTHLY',
    token_limit         BIGINT NOT NULL DEFAULT 0,
    request_limit       BIGINT NOT NULL DEFAULT 0,
    concurrency_limit   INT NOT NULL DEFAULT 0,
    enabled             BOOLEAN NOT NULL DEFAULT TRUE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, user_id, period_type)
);

CREATE TABLE IF NOT EXISTS quota_usage (
    id                  UUID PRIMARY KEY,
    resource_type       TEXT NOT NULL,
    resource_id         TEXT NOT NULL,
    period_type         TEXT NOT NULL,
    period_key          TEXT NOT NULL,
    tokens_used         BIGINT NOT NULL DEFAULT 0,
    requests_used       BIGINT NOT NULL DEFAULT 0,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (resource_type, resource_id, period_type, period_key)
);

CREATE TABLE IF NOT EXISTS token_usage (
    id                      UUID PRIMARY KEY,
    user_id                 UUID,
    workstation_id          TEXT,
    digital_employee_id     TEXT,
    provider                TEXT NOT NULL DEFAULT '',
    model                   TEXT NOT NULL DEFAULT '',
    input_tokens            BIGINT NOT NULL DEFAULT 0,
    output_tokens           BIGINT NOT NULL DEFAULT 0,
    total_tokens            BIGINT NOT NULL DEFAULT 0,
    request_id              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_token_usage_user ON token_usage(user_id);
CREATE INDEX IF NOT EXISTS idx_token_usage_ws ON token_usage(workstation_id);
CREATE INDEX IF NOT EXISTS idx_token_usage_emp ON token_usage(digital_employee_id);

-- ---------- 回填：现有 admin → 成员 OWNER + employee owner ----------
-- 取最早创建的 ADMIN/SUPER_ADMIN 用户作为回填归属
WITH admin_user AS (
    SELECT u.id
    FROM users u
    JOIN user_roles ur ON ur.user_id = u.id
    JOIN roles r ON r.id = ur.role_id
    WHERE r.name IN ('ADMIN', 'SUPER_ADMIN')
    ORDER BY u.created_at ASC
    LIMIT 1
)
UPDATE workstations w
SET created_by_user_id = (SELECT id FROM admin_user)
WHERE created_by_user_id IS NULL
  AND EXISTS (SELECT 1 FROM admin_user);

INSERT INTO workstation_users (id, workstation_id, user_id, role, status, created_at, updated_at)
SELECT gen_random_uuid(), w.id, a.id, 'OWNER', 'active', NOW(), NOW()
FROM workstations w
CROSS JOIN (
    SELECT u.id
    FROM users u
    JOIN user_roles ur ON ur.user_id = u.id
    JOIN roles r ON r.id = ur.role_id
    WHERE r.name IN ('ADMIN', 'SUPER_ADMIN')
    ORDER BY u.created_at ASC
    LIMIT 1
) a
ON CONFLICT (workstation_id, user_id) DO NOTHING;

UPDATE employees e
SET owner_user_id = (
    SELECT u.id
    FROM users u
    JOIN user_roles ur ON ur.user_id = u.id
    JOIN roles r ON r.id = ur.role_id
    WHERE r.name IN ('ADMIN', 'SUPER_ADMIN')
    ORDER BY u.created_at ASC
    LIMIT 1
)
WHERE owner_user_id IS NULL
  AND EXISTS (
    SELECT 1 FROM users u
    JOIN user_roles ur ON ur.user_id = u.id
    JOIN roles r ON r.id = ur.role_id
    WHERE r.name IN ('ADMIN', 'SUPER_ADMIN')
  );

-- +goose Down
DROP TABLE IF EXISTS token_usage;
DROP TABLE IF EXISTS quota_usage;
DROP TABLE IF EXISTS workstation_user_quotas;
DROP TABLE IF EXISTS quota_policies;
DROP INDEX IF EXISTS idx_employees_owner;
ALTER TABLE employees DROP COLUMN IF EXISTS owner_user_id;
DROP TABLE IF EXISTS workstation_users;
ALTER TABLE workstations DROP COLUMN IF EXISTS created_by_user_id;
ALTER TABLE role_permissions DROP COLUMN IF EXISTS scope;
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
ALTER TABLE users DROP COLUMN IF EXISTS last_login_at;
ALTER TABLE users DROP COLUMN IF EXISTS email;
ALTER TABLE users DROP COLUMN IF EXISTS status;
