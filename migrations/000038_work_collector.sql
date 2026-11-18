-- +goose Up
ALTER TABLE mcp_tokens ADD COLUMN service TEXT NOT NULL DEFAULT 'workflow-mcp';
ALTER TABLE mcp_external_user_policies ADD COLUMN collector_can_write BOOLEAN NOT NULL DEFAULT FALSE;
INSERT INTO mcp_servers (id, name, description, server_type, transport, endpoint, config, status)
VALUES ('mcp-collector','personal-work-collector','个人工作采集、飞书内容检索与操作','builtin','http','/mcp/collector','{}','active')
ON CONFLICT (id) DO NOTHING;

-- JSON documents retain a typed application schema, with owner enforced in every key.
CREATE TABLE collector_records (
    owner_id TEXT NOT NULL,
    kind TEXT NOT NULL,
    id TEXT NOT NULL,
    body JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY(owner_id, kind, id)
);
CREATE INDEX collector_records_kind ON collector_records(kind, owner_id, updated_at DESC);
CREATE TABLE collector_leases (
    key TEXT PRIMARY KEY,
    holder TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE collector_leases;
DROP TABLE collector_records;
DELETE FROM employee_mcp_bindings WHERE mcp_server_id='mcp-collector';
DELETE FROM mcp_servers WHERE id='mcp-collector';
ALTER TABLE mcp_external_user_policies DROP COLUMN collector_can_write;
DELETE FROM mcp_tokens WHERE service='personal-work-collector';
ALTER TABLE mcp_tokens DROP COLUMN service;
