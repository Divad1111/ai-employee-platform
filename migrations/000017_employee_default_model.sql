-- +goose Up
-- 数字员工选定的模型，具体可选项由工作站心跳上报。
ALTER TABLE employees ADD COLUMN IF NOT EXISTS default_model TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE employees DROP COLUMN IF EXISTS default_model;
