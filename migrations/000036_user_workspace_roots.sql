-- +goose Up
ALTER TABLE workstations ADD COLUMN workspace_roots jsonb NOT NULL DEFAULT '[]';
-- Keep the first login name even after account renames or workspace deletion.
CREATE TABLE workspace_user_directories (
    user_id UUID PRIMARY KEY REFERENCES users(id),
    directory_name TEXT NOT NULL
);
CREATE UNIQUE INDEX workspace_directory_name_ci ON workspace_user_directories (lower(directory_name));
ALTER TABLE workspaces ADD COLUMN root_path TEXT NOT NULL DEFAULT '',
    ADD COLUMN owner_user_id UUID REFERENCES workspace_user_directories(user_id);
CREATE UNIQUE INDEX workspace_user_project ON workspaces(workstation_id, root_path, owner_user_id, lower(path)) WHERE owner_user_id IS NOT NULL;
-- +goose Down
DROP INDEX workspace_user_project;
ALTER TABLE workspaces DROP COLUMN owner_user_id, DROP COLUMN root_path;
DROP TABLE workspace_user_directories;
ALTER TABLE workstations DROP COLUMN workspace_roots;
