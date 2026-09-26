-- +goose Up
-- Center Server 备份系统数据表与权限初始化
-- 设计依据：docs/AI_Employee_Center_Server_Backup_Design.md §42, §56

-- 1. 备份存储目标表
CREATE TABLE IF NOT EXISTS backup_destinations (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    type                TEXT NOT NULL, -- LOCAL | S3 | SFTP | SMB
    config_encrypted    TEXT NOT NULL DEFAULT '', -- AES-256-GCM 加密的配置及凭据 JSON
    enabled             BOOLEAN NOT NULL DEFAULT FALSE,
    last_test_at        TIMESTAMPTZ,
    last_test_status    TEXT NOT NULL DEFAULT '', -- SUCCESS | FAILED
    last_test_message   TEXT NOT NULL DEFAULT '',
    created_by          TEXT NOT NULL DEFAULT '',
    updated_by          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_destinations_enabled ON backup_destinations(enabled);

-- 2. 备份策略表
CREATE TABLE IF NOT EXISTS backup_policies (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    enabled                 BOOLEAN NOT NULL DEFAULT FALSE,
    scope                   TEXT NOT NULL DEFAULT 'CENTER_FULL', -- 第一阶段 CENTER_FULL
    schedule_type           TEXT NOT NULL DEFAULT 'CRON',       -- MANUAL | CRON
    cron_expression         TEXT NOT NULL DEFAULT '0 2 * * *',
    timezone                TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    retention_keep_last     INT NOT NULL DEFAULT 7,
    compression_algorithm   TEXT NOT NULL DEFAULT 'zstd',       -- zstd | gzip
    compression_level       INT NOT NULL DEFAULT 3,
    encryption_enabled      BOOLEAN NOT NULL DEFAULT TRUE,      -- 是否加密：支持用户配置不加密
    encryption_algorithm    TEXT NOT NULL DEFAULT 'AES-256-GCM',
    encryption_key_version  INT NOT NULL DEFAULT 1,
    next_run_at             TIMESTAMPTZ,
    last_run_at             TIMESTAMPTZ,
    last_run_status         TEXT NOT NULL DEFAULT '',           -- SUCCESS | PARTIAL_SUCCESS | FAILED
    created_by              TEXT NOT NULL DEFAULT '',
    updated_by              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_policies_enabled ON backup_policies(enabled);

-- 3. 策略与目标关联表（多对多）
CREATE TABLE IF NOT EXISTS backup_policy_destinations (
    policy_id       TEXT NOT NULL REFERENCES backup_policies(id) ON DELETE CASCADE,
    destination_id  TEXT NOT NULL REFERENCES backup_destinations(id) ON DELETE RESTRICT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (policy_id, destination_id)
);

-- 4. 备份执行记录表
CREATE TABLE IF NOT EXISTS backup_runs (
    id              TEXT PRIMARY KEY,
    policy_id       TEXT NOT NULL DEFAULT '',
    policy_name     TEXT NOT NULL DEFAULT '',
    backup_id       TEXT NOT NULL UNIQUE,
    scope           TEXT NOT NULL DEFAULT 'CENTER_FULL',
    status          TEXT NOT NULL DEFAULT 'PENDING', -- PENDING | RUNNING | PACKAGING | ENCRYPTING | UPLOADING | VERIFYING | SUCCESS | PARTIAL_SUCCESS | FAILED | CANCELLED
    trigger_type    TEXT NOT NULL DEFAULT 'MANUAL',  -- MANUAL | SCHEDULED
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    duration_ms     BIGINT NOT NULL DEFAULT 0,
    artifact_size   BIGINT NOT NULL DEFAULT 0,
    file_count      INT NOT NULL DEFAULT 0,
    checksum        TEXT NOT NULL DEFAULT '',         -- SHA-256 Checksum
    manifest_json   JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_code      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_runs_policy ON backup_runs(policy_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_status ON backup_runs(status);
CREATE INDEX IF NOT EXISTS idx_backup_runs_created ON backup_runs(created_at DESC);

-- 5. 备份目标分发执行记录表
CREATE TABLE IF NOT EXISTS backup_run_destinations (
    id              TEXT PRIMARY KEY,
    backup_run_id   TEXT NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    destination_id  TEXT NOT NULL REFERENCES backup_destinations(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'PENDING', -- PENDING | RUNNING | UPLOADING | VERIFYING | SUCCESS | FAILED
    remote_path     TEXT NOT NULL DEFAULT '',
    remote_size     BIGINT NOT NULL DEFAULT 0,
    uploaded_at     TIMESTAMPTZ,
    verified_at     TIMESTAMPTZ,
    error_code      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_backup_run_destinations_run ON backup_run_destinations(backup_run_id);

-- 6. 恢复任务记录表
CREATE TABLE IF NOT EXISTS backup_restore_jobs (
    id                      TEXT PRIMARY KEY,
    backup_run_id           TEXT NOT NULL REFERENCES backup_runs(id) ON DELETE CASCADE,
    emergency_backup_run_id TEXT NOT NULL DEFAULT '', -- 恢复前自动创建的系统应急快照 ID
    status                  TEXT NOT NULL DEFAULT 'PENDING', -- PENDING | RUNNING | RESTORING_DB | RESTORING_DATA | VERIFYING | SUCCESS | FAILED
    started_at              TIMESTAMPTZ,
    completed_at            TIMESTAMPTZ,
    created_by              TEXT NOT NULL DEFAULT '',
    error_code              TEXT NOT NULL DEFAULT '',
    error_message           TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_backup_restore_jobs_created ON backup_restore_jobs(created_at DESC);

-- 7. 预置备份系统权限码
INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000070', 'backup.view', '查看备份策略、存储目标与备份记录'),
    ('00000000-0000-0000-0001-000000000071', 'backup.create', '手动执行 Center Server 备份'),
    ('00000000-0000-0000-0001-000000000072', 'backup.manage', '创建与管理备份策略、调度与保留设置'),
    ('00000000-0000-0000-0001-000000000073', 'backup.destination', '创建、修改与测试备份存储目标'),
    ('00000000-0000-0000-0001-000000000074', 'backup.verify', '手动触发备份产物完整性与哈希校验'),
    ('00000000-0000-0000-0001-000000000075', 'backup.delete', '删除指定备份产物与文件'),
    ('00000000-0000-0000-0001-000000000076', 'backup.restore', '执行系统全量容灾恢复（高危特权）')
ON CONFLICT (code) DO NOTHING;

-- ADMIN 角色赋予全部备份管理特权
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'ADMIN'
  AND p.code IN (
    'backup.view', 'backup.create', 'backup.manage',
    'backup.destination', 'backup.verify', 'backup.delete', 'backup.restore'
  )
ON CONFLICT DO NOTHING;

-- VIEWER 角色赋予查看权限
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'VIEWER'
  AND p.code = 'backup.view'
ON CONFLICT DO NOTHING;

-- 预置一个默认 Local 本地备份存储目标（方便快速开箱使用，默认禁用待管理员启用）
INSERT INTO backup_destinations (id, name, type, config_encrypted, enabled)
VALUES (
    'dest-local-default',
    '默认本地存储',
    'LOCAL',
    '{"path": "/data/backups"}',
    FALSE
) ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS backup_restore_jobs;
DROP TABLE IF EXISTS backup_run_destinations;
DROP TABLE IF EXISTS backup_runs;
DROP TABLE IF EXISTS backup_policy_destinations;
DROP TABLE IF EXISTS backup_policies;
DROP TABLE IF EXISTS backup_destinations;
