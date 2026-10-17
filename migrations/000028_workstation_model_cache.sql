-- +goose Up
ALTER TABLE workstations
    ADD COLUMN IF NOT EXISTS model_options_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS model_options_updated_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE workstations
    DROP COLUMN IF EXISTS model_options_updated_at,
    DROP COLUMN IF EXISTS model_options_json;
