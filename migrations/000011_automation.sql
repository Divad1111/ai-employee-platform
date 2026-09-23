-- +goose Up
-- 自动化任务：周期 / 日历链 / Webhook 触发层（与 Job 派发 Scheduler 分离）

CREATE TABLE IF NOT EXISTS automations (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    trigger_type    TEXT NOT NULL CHECK (trigger_type IN ('cron', 'calendar', 'webhook')),
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    employee_id     TEXT NOT NULL DEFAULT '',
    prompt          TEXT NOT NULL DEFAULT '',
    timezone        TEXT NOT NULL DEFAULT 'Asia/Shanghai',
    notify_chat_id  TEXT NOT NULL DEFAULT '',
    trigger_config  JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_fired_at   TIMESTAMPTZ,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_automations_type_enabled ON automations(trigger_type, enabled);

CREATE TABLE IF NOT EXISTS automation_calendar_items (
    id              TEXT PRIMARY KEY,
    automation_id   TEXT NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    run_date        DATE NOT NULL,
    seq             INT NOT NULL CHECK (seq >= 1),
    employee_id     TEXT NOT NULL,
    prompt          TEXT NOT NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (automation_id, run_date, seq)
);

CREATE INDEX IF NOT EXISTS idx_auto_cal_items_date ON automation_calendar_items(automation_id, run_date);

CREATE TABLE IF NOT EXISTS automation_runs (
    id                  TEXT PRIMARY KEY,
    automation_id       TEXT NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    calendar_item_id    TEXT,
    job_id              TEXT,
    status              TEXT NOT NULL DEFAULT 'triggered',
    trigger_source      TEXT NOT NULL DEFAULT '',
    idempotency_key     TEXT NOT NULL DEFAULT '',
    error               TEXT NOT NULL DEFAULT '',
    payload             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at         TIMESTAMPTZ
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_auto_runs_idem ON automation_runs(idempotency_key) WHERE idempotency_key <> '';
CREATE INDEX IF NOT EXISTS idx_auto_runs_auto ON automation_runs(automation_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_auto_runs_job ON automation_runs(job_id) WHERE job_id IS NOT NULL AND job_id <> '';

-- 权限：automation.read 可读；automation.write 仅 ADMIN
INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000024', 'automation.read', '读取自动化任务'),
    ('00000000-0000-0000-0001-000000000025', 'automation.write', '创建/修改自动化任务与 Webhook 配置')
ON CONFLICT (code) DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'ADMIN'
  AND p.code IN ('automation.read', 'automation.write')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'OPERATOR'
  AND p.code IN ('automation.read')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'VIEWER'
  AND p.code IN ('automation.read')
ON CONFLICT DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS automation_runs;
DROP TABLE IF EXISTS automation_calendar_items;
DROP TABLE IF EXISTS automations;
-- 权限码保留，避免再次出现 fallback 空洞
SELECT 1;
