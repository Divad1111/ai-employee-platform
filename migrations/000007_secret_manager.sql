-- +goose Up
-- Secret Manager 增强（M8）：元数据、轮换时间、绑定用途索引
-- 设计依据：§62、§26、§88

ALTER TABLE secrets ADD COLUMN IF NOT EXISTS description TEXT NOT NULL DEFAULT '';
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS rotated_at TIMESTAMPTZ;
ALTER TABLE secrets ADD COLUMN IF NOT EXISTS created_by TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_secret_refs_employee ON secret_references(employee_id);
CREATE INDEX IF NOT EXISTS idx_secret_refs_secret ON secret_references(secret_id);

-- +goose Down
DROP INDEX IF EXISTS idx_secret_refs_secret;
DROP INDEX IF EXISTS idx_secret_refs_employee;
