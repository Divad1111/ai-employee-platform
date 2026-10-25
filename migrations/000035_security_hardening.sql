-- +goose Up
-- Dedicated stores use text identifiers, matching runtime IDs.
CREATE TABLE security_state (
 kind TEXT NOT NULL, id TEXT NOT NULL, payload JSONB NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), PRIMARY KEY(kind,id)
);
CREATE TABLE security_enrollment_tokens (
 id TEXT PRIMARY KEY, token_hash TEXT NOT NULL UNIQUE, payload JSONB NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL, used_at TIMESTAMPTZ
);
CREATE TABLE security_node_reservations(workstation_id TEXT PRIMARY KEY,token_id TEXT NOT NULL DEFAULT '',created_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
INSERT INTO security_node_reservations(workstation_id) SELECT id FROM workstations;
CREATE TABLE security_replay_keys (key TEXT PRIMARY KEY, expires_at TIMESTAMPTZ NOT NULL);
CREATE INDEX security_replay_expiry ON security_replay_keys(expires_at);
-- Preserve persisted permission rules rather than overwrite them with defaults.
INSERT INTO security_state(kind,id,payload)
 SELECT 'permission.profiles',id,to_jsonb(p) FROM permission_profiles p;
INSERT INTO security_state(kind,id,payload)
 SELECT 'permission.rules',profile_id || ':' || action,to_jsonb(r) FROM permission_rules r;
-- Bootstrap is permanently closed once the first user has existed, including soft deletes.
INSERT INTO system_config(key,value_json)
 SELECT 'security.initialized','true'::jsonb WHERE EXISTS(SELECT 1 FROM users)
 ON CONFLICT(key) DO NOTHING;
-- Existing sessions are intentionally revoked on first upgrade to hashed tokens.
CREATE TABLE IF NOT EXISTS admin_web_sessions (
 token TEXT PRIMARY KEY,user_id TEXT NOT NULL,username TEXT NOT NULL,
 roles TEXT[] NOT NULL DEFAULT '{}',expires_at TIMESTAMPTZ NOT NULL,step_up_until TIMESTAMPTZ
);
ALTER TABLE admin_web_sessions ADD COLUMN IF NOT EXISTS credential_version TEXT NOT NULL DEFAULT '';
ALTER TABLE admin_web_sessions ADD COLUMN IF NOT EXISTS mfa_authenticated BOOLEAN NOT NULL DEFAULT FALSE;
DELETE FROM admin_web_sessions WHERE token NOT LIKE 'sha256:%';
-- Reduce the built-in USER role once. Future startups do not rewrite grants.
DELETE FROM role_permissions rp USING roles r, permissions p
 WHERE rp.role_id=r.id AND rp.permission_id=p.id AND r.name='USER'
 AND p.code NOT IN (
 'employee.read','employee.write','employee.delete','workstation.read','workstation.write',
 'workspace.read','workspace.write','session.read','session.write','job.read','job.write','job.cancel',
 'message.read','message.write','approval.read','secret.read','secret.write',
 'enrollment.write','workflow.read','workflow.write','workflow.delete','workflow.grant',
 'automation.read','automation.write','user.read','user.update','quota.read');
-- +goose Down
-- Security hardening is not automatically reversed; rollback must preserve audit and deny unsafe defaults.
