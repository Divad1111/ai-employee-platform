-- +goose Up
-- 任务 Token Usage 明细（按 Run）与 Job 汇总缓存扩展。
-- 权威数据在 job_token_usage；jobs 表字段仅为汇总缓存。

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cached_input_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cache_write_input_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cache_read_input_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS reasoning_output_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS total_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS token_usage_status TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS job_token_usage (
    id                        TEXT PRIMARY KEY,
    job_id                    TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    workstation_id            TEXT NOT NULL DEFAULT '',
    digital_employee_id       TEXT NOT NULL DEFAULT '',
    provider                  TEXT NOT NULL DEFAULT '',
    provider_session_id       TEXT NOT NULL DEFAULT '',
    provider_run_id           TEXT NOT NULL DEFAULT '',
    input_tokens              BIGINT NOT NULL DEFAULT 0,
    cached_input_tokens       BIGINT NOT NULL DEFAULT 0,
    cache_write_input_tokens  BIGINT NOT NULL DEFAULT 0,
    cache_read_input_tokens   BIGINT NOT NULL DEFAULT 0,
    output_tokens             BIGINT NOT NULL DEFAULT 0,
    reasoning_output_tokens   BIGINT NOT NULL DEFAULT 0,
    total_tokens              BIGINT NOT NULL DEFAULT 0,
    usage_status              TEXT NOT NULL DEFAULT 'UNAVAILABLE',
    usage_source              TEXT NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_job_token_usage_provider_run
    ON job_token_usage(provider, provider_run_id)
    WHERE provider_run_id IS NOT NULL AND provider_run_id <> '';

CREATE INDEX IF NOT EXISTS idx_job_token_usage_job ON job_token_usage(job_id);

-- +goose Down
DROP INDEX IF EXISTS idx_job_token_usage_job;
DROP INDEX IF EXISTS idx_job_token_usage_provider_run;
DROP TABLE IF EXISTS job_token_usage;
ALTER TABLE jobs DROP COLUMN IF EXISTS token_usage_status;
ALTER TABLE jobs DROP COLUMN IF EXISTS total_tokens;
ALTER TABLE jobs DROP COLUMN IF EXISTS reasoning_output_tokens;
ALTER TABLE jobs DROP COLUMN IF EXISTS cache_read_input_tokens;
ALTER TABLE jobs DROP COLUMN IF EXISTS cache_write_input_tokens;
ALTER TABLE jobs DROP COLUMN IF EXISTS cached_input_tokens;
