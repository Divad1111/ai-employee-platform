-- +goose Up
ALTER TABLE employees
    ADD COLUMN IF NOT EXISTS session_generation BIGINT NOT NULL DEFAULT 0;

ALTER TABLE task_conversations
    ADD COLUMN IF NOT EXISTS merged_into_id TEXT,
    ADD COLUMN IF NOT EXISTS active_provider TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS active_model TEXT NOT NULL DEFAULT '';

-- +goose Down
DROP INDEX IF EXISTS idx_task_conversations_employee_active;
ALTER TABLE task_conversations DROP COLUMN IF EXISTS merged_into_id;
ALTER TABLE task_conversations DROP COLUMN IF EXISTS active_provider;
ALTER TABLE task_conversations DROP COLUMN IF EXISTS active_model;
ALTER TABLE employees DROP COLUMN IF EXISTS session_generation;
