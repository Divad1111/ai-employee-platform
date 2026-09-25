-- +goose Up
-- 日历任务链：当天几点开始执行（HH:MM，按自动化规则时区）。空表示不限制时刻。
ALTER TABLE automation_calendar_items ADD COLUMN IF NOT EXISTS run_clock TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE automation_calendar_items DROP COLUMN IF EXISTS run_clock;
