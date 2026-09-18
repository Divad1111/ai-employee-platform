-- +goose Up
-- employees 及绑定表
-- 设计依据：§2、§26、§32、§33、§82

CREATE TABLE IF NOT EXISTS employees (
    id                  TEXT PRIMARY KEY,
    name                TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    role_summary        TEXT NOT NULL DEFAULT '',
    default_provider    TEXT NOT NULL DEFAULT '',
    workstation_id      TEXT,
    workspace_id        TEXT,
    permission_profile  TEXT NOT NULL DEFAULT '',
    runtime_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    session_policy_json JSONB NOT NULL DEFAULT '{}'::jsonb,
    status              TEXT NOT NULL DEFAULT 'STOPPED',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS skills (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS knowledge (
    id          UUID PRIMARY KEY,
    title       TEXT NOT NULL,
    body        TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS employee_skills (
    employee_id TEXT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    skill_id    UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    PRIMARY KEY (employee_id, skill_id)
);

CREATE TABLE IF NOT EXISTS employee_knowledge (
    employee_id  TEXT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    knowledge_id UUID NOT NULL REFERENCES knowledge(id) ON DELETE CASCADE,
    PRIMARY KEY (employee_id, knowledge_id)
);

-- 单 Bot 路由：绑定飞书身份到 Employee（决策 Q-02）
CREATE TABLE IF NOT EXISTS employee_feishu_bindings (
    id           UUID PRIMARY KEY,
    employee_id  TEXT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    feishu_open_id TEXT,
    feishu_bot_alias TEXT,
    chat_id      TEXT,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (employee_id, feishu_bot_alias)
);

CREATE INDEX IF NOT EXISTS idx_employee_feishu_alias ON employee_feishu_bindings(feishu_bot_alias);

-- +goose Down
DROP TABLE IF EXISTS employee_feishu_bindings;
DROP TABLE IF EXISTS employee_knowledge;
DROP TABLE IF EXISTS employee_skills;
DROP TABLE IF EXISTS knowledge;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS employees;
