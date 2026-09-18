-- +goose Up
-- jobs / sessions / messages / conversations
-- 设计依据：§14、§15、§26、§83、§84、§112、§113

CREATE TABLE IF NOT EXISTS conversations (
    id          UUID PRIMARY KEY,
    channel     TEXT NOT NULL DEFAULT '',
    external_id TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS messages (
    id               UUID PRIMARY KEY,
    conversation_id  UUID REFERENCES conversations(id) ON DELETE SET NULL,
    sender_type      TEXT NOT NULL,
    sender_id        TEXT NOT NULL,
    receiver_type    TEXT NOT NULL,
    receiver_id      TEXT NOT NULL,
    content          TEXT NOT NULL DEFAULT '',
    attachments_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    reply_to         UUID REFERENCES messages(id) ON DELETE SET NULL,
    -- 飞书去重
    feishu_event_id  TEXT,
    feishu_message_id TEXT,
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_feishu_event
    ON messages(feishu_event_id) WHERE feishu_event_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_feishu_msg
    ON messages(feishu_message_id) WHERE feishu_message_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS sessions (
    id               TEXT PRIMARY KEY,
    employee_id      TEXT NOT NULL REFERENCES employees(id),
    workstation_id   TEXT REFERENCES workstations(id),
    workspace_id     TEXT REFERENCES workspaces(id),
    provider         TEXT NOT NULL DEFAULT '',
    process_id       INT,
    status           TEXT NOT NULL DEFAULT 'STOPPED',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at       TIMESTAMPTZ,
    last_activity_at TIMESTAMPTZ,
    ended_at         TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS jobs (
    id               TEXT PRIMARY KEY,
    employee_id      TEXT NOT NULL REFERENCES employees(id),
    workspace_id     TEXT REFERENCES workspaces(id),
    session_id       TEXT REFERENCES sessions(id),
    workstation_id   TEXT REFERENCES workstations(id),
    prompt           TEXT NOT NULL DEFAULT '',
    attachments_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    parent_job_id    TEXT REFERENCES jobs(id),
    created_by       TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'CREATED',
    result           TEXT NOT NULL DEFAULT '',
    idempotency_key  TEXT NOT NULL,
    timeout_sec      INT NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    CONSTRAINT jobs_idempotency_key_unique UNIQUE (idempotency_key)
);

CREATE TABLE IF NOT EXISTS job_events (
    id          BIGSERIAL PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_job_events_job ON job_events(job_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS job_events;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS conversations;
