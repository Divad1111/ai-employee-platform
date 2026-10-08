-- +goose Up
-- 删除工作区时保留历史会话和任务，只把工作区引用置空。

ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_workspace_id_fkey;
ALTER TABLE sessions
    ADD CONSTRAINT sessions_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL;

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_workspace_id_fkey;
ALTER TABLE jobs
    ADD CONSTRAINT jobs_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_workspace_id_fkey;
ALTER TABLE sessions
    ADD CONSTRAINT sessions_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id);

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_workspace_id_fkey;
ALTER TABLE jobs
    ADD CONSTRAINT jobs_workspace_id_fkey
    FOREIGN KEY (workspace_id) REFERENCES workspaces(id);
