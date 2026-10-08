-- +goose Up
-- 公用工作站：创建者可指定哪些平台角色默认可以使用。

ALTER TABLE workstations ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT FALSE;

CREATE TABLE IF NOT EXISTS workstation_role_grants (
    workstation_id TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    role_name      TEXT NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (workstation_id, role_name)
);

-- +goose Down
DROP TABLE IF EXISTS workstation_role_grants;
ALTER TABLE workstations DROP COLUMN IF EXISTS is_public;
