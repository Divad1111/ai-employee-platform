-- +goose Up
-- 制品表：ID 与 idgen(ART-*) 对齐为 TEXT；补索引与上传者元数据。

ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_pkey;
ALTER TABLE artifacts ALTER COLUMN id TYPE TEXT USING id::text;
ALTER TABLE artifacts ADD PRIMARY KEY (id);

ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS uploaded_by TEXT NOT NULL DEFAULT '';
ALTER TABLE artifacts ADD COLUMN IF NOT EXISTS workstation_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_artifacts_job_id ON artifacts (job_id);
CREATE INDEX IF NOT EXISTS idx_artifacts_sha256 ON artifacts (sha256);
CREATE INDEX IF NOT EXISTS idx_artifacts_created_at ON artifacts (created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_artifacts_created_at;
DROP INDEX IF EXISTS idx_artifacts_sha256;
DROP INDEX IF EXISTS idx_artifacts_job_id;
ALTER TABLE artifacts DROP COLUMN IF EXISTS workstation_id;
ALTER TABLE artifacts DROP COLUMN IF EXISTS uploaded_by;
-- Down 不回退 id 类型，避免破坏已写入的 ART-* 行。
