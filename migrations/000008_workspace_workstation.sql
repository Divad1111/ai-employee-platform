-- +goose Up
-- 工作区关联工作站节点：路径属于指定计算节点。
-- 设计依据：§50 Workspace；Admin 创建流程：先选工作站再填本机路径。

ALTER TABLE workspaces
    ADD COLUMN IF NOT EXISTS workstation_id TEXT REFERENCES workstations(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_workspaces_workstation ON workspaces(workstation_id);

-- +goose Down
DROP INDEX IF EXISTS idx_workspaces_workstation;
ALTER TABLE workspaces DROP COLUMN IF EXISTS workstation_id;
