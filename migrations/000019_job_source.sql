-- +goose Up
-- 任务来源：标识任务创建来源（如 feishu, cron, calendar, webhook, web, api 等，开放扩展）。
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'web';
CREATE INDEX IF NOT EXISTS idx_jobs_source ON jobs(source);

-- +goose Down
DROP INDEX IF EXISTS idx_jobs_source;
ALTER TABLE jobs DROP COLUMN IF EXISTS source;
