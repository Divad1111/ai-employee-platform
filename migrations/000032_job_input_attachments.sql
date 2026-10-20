-- +goose Up
ALTER TABLE jobs ADD COLUMN input_attachments JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose Down
ALTER TABLE jobs DROP COLUMN input_attachments;
