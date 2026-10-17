-- +goose Up
-- 飞书任务卡片以用户任务会话为单位；ACP Session 可以在不同会话间复用。
CREATE TABLE IF NOT EXISTS task_conversations (
    id               TEXT PRIMARY KEY,
    employee_id      TEXT NOT NULL REFERENCES employees(id),
    employee_name    TEXT NOT NULL DEFAULT '',
    owner_user_id    TEXT NOT NULL DEFAULT '',
    owner_open_id    TEXT NOT NULL DEFAULT '',
    chat_id          TEXT NOT NULL DEFAULT '',
    current_job_id   TEXT REFERENCES jobs(id) ON DELETE SET NULL,
    card_message_id  TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'CREATED',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS task_conversation_turns (
    conversation_id TEXT NOT NULL REFERENCES task_conversations(id) ON DELETE CASCADE,
    job_id          TEXT NOT NULL UNIQUE REFERENCES jobs(id) ON DELETE CASCADE,
    turn_no         INT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'CREATED',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (conversation_id, turn_no)
);

CREATE TABLE IF NOT EXISTS task_conversation_messages (
    id              BIGSERIAL PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES task_conversations(id) ON DELETE CASCADE,
    job_id          TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    role            TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'RUNNING',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, role)
);

CREATE TABLE IF NOT EXISTS task_conversation_deltas (
    event_id        TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES task_conversations(id) ON DELETE CASCADE,
    job_id          TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence        BIGINT NOT NULL,
    content         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_task_conversation_messages_order
    ON task_conversation_messages(conversation_id, id);
CREATE INDEX IF NOT EXISTS idx_task_conversation_deltas_order
    ON task_conversation_deltas(job_id, sequence);

-- +goose Down
DROP TABLE IF EXISTS task_conversation_deltas;
DROP TABLE IF EXISTS task_conversation_messages;
DROP TABLE IF EXISTS task_conversation_turns;
DROP TABLE IF EXISTS task_conversations;
