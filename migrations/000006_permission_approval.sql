-- +goose Up
-- Permission Engine / Approval / TOTP（M7）
-- 设计依据：§28–§30、§60、§113

CREATE TABLE IF NOT EXISTS permission_profiles (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS permission_rules (
    id          UUID PRIMARY KEY,
    profile_id  TEXT NOT NULL REFERENCES permission_profiles(id) ON DELETE CASCADE,
    action      TEXT NOT NULL,
    effect      TEXT NOT NULL CHECK (effect IN ('ALLOW', 'ASK', 'DENY')),
    critical    BOOLEAN NOT NULL DEFAULT FALSE,
    priority    INT NOT NULL DEFAULT 100,
    description TEXT NOT NULL DEFAULT '',
    UNIQUE (profile_id, action)
);

CREATE INDEX IF NOT EXISTS idx_permission_rules_profile ON permission_rules(profile_id);

-- 扩展 approval_requests（若已存在则补列）
ALTER TABLE approval_requests ADD COLUMN IF NOT EXISTS employee_id TEXT;
ALTER TABLE approval_requests ADD COLUMN IF NOT EXISTS requester_id TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN IF NOT EXISTS effect TEXT NOT NULL DEFAULT 'ASK';
ALTER TABLE approval_requests ADD COLUMN IF NOT EXISTS reason TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN IF NOT EXISTS resolved_by TEXT NOT NULL DEFAULT '';
ALTER TABLE approval_requests ADD COLUMN IF NOT EXISTS critical BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS approval_policies (
    id          TEXT PRIMARY KEY,
    action      TEXT NOT NULL UNIQUE,
    require_totp BOOLEAN NOT NULL DEFAULT FALSE,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS user_totp (
    user_id     UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    secret_ref  TEXT NOT NULL, -- Secret Vault 引用，禁止明文列
    enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    failed_count INT NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 默认 Profile 与 §29 规则（固定 UUID 便于幂等）
INSERT INTO permission_profiles (id, name, description, is_default) VALUES
    ('default', 'Default', 'V2 默认权限配置（未匹配规则 DENY）', TRUE)
ON CONFLICT (id) DO NOTHING;

INSERT INTO permission_rules (id, profile_id, action, effect, critical, priority, description) VALUES
    ('00000000-0000-4000-8000-000000000001', 'default', 'workspace.read', 'ALLOW', FALSE, 10, 'Workspace 读取'),
    ('00000000-0000-4000-8000-000000000002', 'default', 'workspace.write', 'ALLOW', FALSE, 10, 'Workspace 修改'),
    ('00000000-0000-4000-8000-000000000003', 'default', 'git.status', 'ALLOW', FALSE, 10, 'Git status'),
    ('00000000-0000-4000-8000-000000000004', 'default', 'git.commit', 'ALLOW', FALSE, 10, 'Git commit'),
    ('00000000-0000-4000-8000-000000000005', 'default', 'git.push', 'ASK', TRUE, 10, 'Git push'),
    ('00000000-0000-4000-8000-000000000006', 'default', 'fs.delete_bulk', 'ASK', FALSE, 10, '删除大量文件'),
    ('00000000-0000-4000-8000-000000000007', 'default', 'shell.powershell', 'ASK', FALSE, 10, 'PowerShell'),
    ('00000000-0000-4000-8000-000000000008', 'default', 'fs.system_modify', 'DENY', TRUE, 10, '系统文件修改'),
    ('00000000-0000-4000-8000-000000000009', 'default', 'system.shutdown', 'DENY', TRUE, 10, 'shutdown'),
    ('00000000-0000-4000-8000-00000000000a', 'default', 'deploy.production', 'ASK', TRUE, 10, 'Production Deploy'),
    ('00000000-0000-4000-8000-00000000000b', 'default', 'workspace.delete', 'ASK', TRUE, 10, 'Delete Workspace'),
    ('00000000-0000-4000-8000-00000000000c', 'default', 'credential.rotate', 'ASK', TRUE, 10, 'Rotate Credential')
ON CONFLICT (profile_id, action) DO NOTHING;

INSERT INTO approval_policies (id, action, require_totp, description) VALUES
    ('pol-git-push', 'git.push', TRUE, 'Git Push 需 TOTP'),
    ('pol-deploy', 'deploy.production', TRUE, '生产部署需 TOTP'),
    ('pol-ws-del', 'workspace.delete', TRUE, '删除 Workspace 需 TOTP'),
    ('pol-cred', 'credential.rotate', TRUE, '轮换凭证需 TOTP')
ON CONFLICT (action) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS user_totp;
DROP TABLE IF EXISTS approval_policies;
DROP TABLE IF EXISTS permission_rules;
DROP TABLE IF EXISTS permission_profiles;
