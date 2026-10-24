-- +goose Up
CREATE TABLE infrastructure_records (
    kind TEXT NOT NULL,
    id TEXT NOT NULL,
    body JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(kind,id)
);
CREATE UNIQUE INDEX infrastructure_release_identity ON infrastructure_records
    ((body->>'software_id'),(body->>'version'),(body->>'os'),(body->>'arch')) WHERE kind='release';
CREATE TABLE infrastructure_operations (
    id TEXT PRIMARY KEY,
    host_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    spec JSONB NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('QUEUED','RUNNING','WAITING','SUCCESS','FAILED','INTERRUPTED','CANCELLED')),
    step TEXT NOT NULL DEFAULT '',
    log TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    not_before TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_until TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX infrastructure_host_busy ON infrastructure_operations(host_id) WHERE status IN ('QUEUED','RUNNING','WAITING');
CREATE INDEX infrastructure_queue ON infrastructure_operations(status,not_before);
INSERT INTO permissions(id,code,description) VALUES
('00000000-0000-0000-0001-000000000090','infra.read','查看 PVE、软件和部署记录'),
('00000000-0000-0000-0001-000000000091','infra.manage','管理 PVE、软件、安装方案和更新策略'),
('00000000-0000-0000-0001-000000000092','infra.deploy','创建虚拟机、部署软件和执行升级回滚'),
('00000000-0000-0000-0001-000000000093','infra.console','登录虚拟机 SSH 终端'),
('00000000-0000-0000-0001-000000000094','infra.credential','管理和查看基础设施凭证')
ON CONFLICT(code) DO NOTHING;
INSERT INTO role_permissions(role_id,permission_id,scope)
SELECT r.id,p.id,'ALL' FROM roles r CROSS JOIN permissions p
WHERE r.name IN ('ADMIN','SUPER_ADMIN') AND p.code LIKE 'infra.%'
ON CONFLICT(role_id,permission_id) DO NOTHING;

-- +goose Down
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE code LIKE 'infra.%');
DELETE FROM permissions WHERE code LIKE 'infra.%';
DROP TABLE infrastructure_operations;
DROP TABLE infrastructure_records;
