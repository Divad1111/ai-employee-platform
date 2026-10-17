-- +goose Up
-- 记录可展示给用户的工具/计划进度摘要，完整消息与增量仍保存在原有表中。
CREATE TABLE IF NOT EXISTS task_conversation_activities (
    event_id        TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES task_conversations(id) ON DELETE CASCADE,
    job_id          TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence        BIGINT NOT NULL,
    content         TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (job_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_task_conversation_activities_order
    ON task_conversation_activities(conversation_id, job_id, sequence);

-- +goose Down
DROP TABLE IF EXISTS task_conversation_activities;
