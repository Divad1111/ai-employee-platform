-- +goose Up
-- providers / audit / system_config / V2 占位表
-- 设计依据：§26、§31、§38、§81

CREATE TABLE IF NOT EXISTS providers (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS provider_versions (
    id            UUID PRIMARY KEY,
    provider_id   TEXT NOT NULL REFERENCES providers(id) ON DELETE CASCADE,
    version       TEXT NOT NULL,
    os            TEXT NOT NULL,
    arch          TEXT NOT NULL,
    download_url  TEXT NOT NULL DEFAULT '',
    sha256        TEXT NOT NULL DEFAULT '',
    signature     TEXT NOT NULL DEFAULT '',
    release_notes TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider_id, version, os, arch)
);

CREATE TABLE IF NOT EXISTS provider_installations (
    id             UUID PRIMARY KEY,
    workstation_id TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    provider_id    TEXT NOT NULL REFERENCES providers(id),
    version        TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT 'NOT_INSTALLED',
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, provider_id)
);

CREATE TABLE IF NOT EXISTS audit_logs (
    id          BIGSERIAL PRIMARY KEY,
    actor_type  TEXT NOT NULL,
    actor_id    TEXT NOT NULL,
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    result      TEXT NOT NULL DEFAULT '',
    ip          TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at DESC);

-- 非 Secret 系统配置；飞书 Secret 仅存引用
CREATE TABLE IF NOT EXISTS system_config (
    key         TEXT PRIMARY KEY,
    value_json  JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- V2 占位：approval / secrets / artifacts（结构先建，逻辑后补）
CREATE TABLE IF NOT EXISTS approval_requests (
    id          UUID PRIMARY KEY,
    job_id      TEXT REFERENCES jobs(id),
    status      TEXT NOT NULL DEFAULT 'PENDING',
    action      TEXT NOT NULL DEFAULT '',
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS secrets (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    -- 仅存引用或密文句柄，禁止业务明文列
    ref         TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS secret_references (
    id          UUID PRIMARY KEY,
    employee_id TEXT REFERENCES employees(id) ON DELETE CASCADE,
    secret_id   UUID NOT NULL REFERENCES secrets(id) ON DELETE CASCADE,
    purpose     TEXT NOT NULL DEFAULT '',
    UNIQUE (employee_id, secret_id, purpose)
);

CREATE TABLE IF NOT EXISTS artifacts (
    id          UUID PRIMARY KEY,
    job_id      TEXT REFERENCES jobs(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT '',
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    sha256      TEXT NOT NULL DEFAULT '',
    storage     TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO providers (id, name, description) VALUES
    ('cursor', 'Cursor', 'Cursor Agent Provider'),
    ('codex', 'Codex', 'Codex Agent Provider')
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS artifacts;
DROP TABLE IF EXISTS secret_references;
DROP TABLE IF EXISTS secrets;
DROP TABLE IF EXISTS approval_requests;
DROP TABLE IF EXISTS system_config;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS provider_installations;
DROP TABLE IF EXISTS provider_versions;
DROP TABLE IF EXISTS providers;
