-- +goose Up
ALTER TABLE employees
    ADD COLUMN IF NOT EXISTS session_context_reset_generation BIGINT NOT NULL DEFAULT -1;

ALTER TABLE task_conversation_turns
    ADD COLUMN IF NOT EXISTS session_generation BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE task_conversation_turns DROP COLUMN IF EXISTS session_generation;
ALTER TABLE employees DROP COLUMN IF EXISTS session_context_reset_generation;
