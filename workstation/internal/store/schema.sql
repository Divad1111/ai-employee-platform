-- Workstation 本地 SQLite 初始 schema（幂等）。
-- 设计依据：§25、§23、§53。
-- 由 store.Migrate 嵌入执行。

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS workstation_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS employees (
    employee_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'STOPPED',
    meta_json   TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS workspaces (
    workspace_id TEXT PRIMARY KEY,
    employee_id  TEXT,
    path         TEXT NOT NULL,
    repository   TEXT NOT NULL DEFAULT '',
    branch       TEXT NOT NULL DEFAULT '',
    meta_json    TEXT NOT NULL DEFAULT '{}'
);

-- 对齐 §53 agent_sessions
CREATE TABLE IF NOT EXISTS sessions (
    session_id       TEXT PRIMARY KEY,
    employee_id      TEXT NOT NULL,
    provider         TEXT NOT NULL DEFAULT '',
    workspace_id     TEXT,
    process_id       INTEGER,
    status           TEXT NOT NULL DEFAULT 'STOPPED',
    created_at       TEXT NOT NULL,
    started_at       TEXT,
    last_activity_at TEXT,
    ended_at         TEXT
);

CREATE TABLE IF NOT EXISTS jobs (
    job_id           TEXT PRIMARY KEY,
    employee_id      TEXT NOT NULL,
    workspace_id     TEXT,
    session_id       TEXT,
    status           TEXT NOT NULL DEFAULT 'CREATED',
    prompt           TEXT NOT NULL DEFAULT '',
    result           TEXT NOT NULL DEFAULT '',
    idempotency_key  TEXT,
    created_at       TEXT NOT NULL,
    started_at       TEXT,
    completed_at     TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_jobs_idempotency
    ON jobs(idempotency_key) WHERE idempotency_key IS NOT NULL AND idempotency_key != '';

CREATE TABLE IF NOT EXISTS job_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id     TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

-- Server → Worker 命令（ACK 前必须持久化）
CREATE TABLE IF NOT EXISTS server_commands (
    command_id   TEXT PRIMARY KEY,
    sequence     INTEGER NOT NULL,
    type         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'RECEIVED',
    acked        INTEGER NOT NULL DEFAULT 0,
    received_at  TEXT NOT NULL,
    acked_at     TEXT
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_server_commands_seq ON server_commands(sequence);

CREATE TABLE IF NOT EXISTS worker_events (
    event_id     TEXT PRIMARY KEY,
    sequence     INTEGER NOT NULL,
    type         TEXT NOT NULL,
    payload      TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL
);

-- Outbox：断网不丢事件
CREATE TABLE IF NOT EXISTS outbox (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id     TEXT NOT NULL UNIQUE,
    sequence     INTEGER NOT NULL,
    payload      TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    sent_at      TEXT,
    status       TEXT NOT NULL DEFAULT 'PENDING'
);

CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox(status, sequence);

CREATE TABLE IF NOT EXISTS provider_installations (
    provider   TEXT PRIMARY KEY,
    version    TEXT NOT NULL DEFAULT '',
    path       TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'NOT_INSTALLED',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS updates (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    version      TEXT NOT NULL,
    status       TEXT NOT NULL,
    created_at   TEXT NOT NULL,
    finished_at  TEXT
);
