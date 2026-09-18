-- +goose Up
-- workstations / certificates / workspaces
-- 设计依据：§18、§26、§50、§126

CREATE TABLE IF NOT EXISTS workstations (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    os               TEXT NOT NULL DEFAULT '',
    arch             TEXT NOT NULL DEFAULT '',
    version          TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'PROVISIONING',
    cpu_percent      DOUBLE PRECISION,
    memory_percent   DOUBLE PRECISION,
    disk_percent     DOUBLE PRECISION,
    capabilities_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_heartbeat_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS workstation_certificates (
    id              UUID PRIMARY KEY,
    workstation_id  TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    fingerprint     TEXT NOT NULL UNIQUE,
    certificate_pem TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'ACTIVE',
    issued_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    revoked_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ws_cert_ws ON workstation_certificates(workstation_id);

CREATE TABLE IF NOT EXISTS workspaces (
    id                 TEXT PRIMARY KEY,
    employee_id        TEXT REFERENCES employees(id) ON DELETE SET NULL,
    path               TEXT NOT NULL,
    repository         TEXT NOT NULL DEFAULT '',
    branch             TEXT NOT NULL DEFAULT '',
    permission_policy  JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- enrollment token（一次性注册）
CREATE TABLE IF NOT EXISTS enrollment_tokens (
    id          UUID PRIMARY KEY,
    token_hash  TEXT NOT NULL UNIQUE,
    label       TEXT NOT NULL DEFAULT '',
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    created_by  UUID REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS enrollment_tokens;
DROP TABLE IF EXISTS workspaces;
DROP TABLE IF EXISTS workstation_certificates;
DROP TABLE IF EXISTS workstations;
