-- +goose Up
-- 多用户、多数字员工、MCP 与身份凭证模型
-- 设计依据：docs/AI_Employee_MultiUser_MCP_Identity_Design.md

-- 1. MCP Server 注册表
CREATE TABLE IF NOT EXISTS mcp_servers (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    server_type     TEXT NOT NULL DEFAULT 'custom', -- builtin | custom
    transport       TEXT NOT NULL DEFAULT 'http',   -- http | sse | stdio
    endpoint        TEXT NOT NULL DEFAULT '',       -- url (http/sse) 或 命令行指令 (stdio)
    config          JSONB NOT NULL DEFAULT '{}'::jsonb, -- headers, env, args, etc.
    status          TEXT NOT NULL DEFAULT 'active', -- active | disabled
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_servers_status ON mcp_servers(status);

-- 预置系统内置 workflow-mcp 实例
INSERT INTO mcp_servers (id, name, description, server_type, transport, endpoint, status)
VALUES (
    'mcp-workflow',
    'workflow-mcp',
    '系统内置工作流、技能包与企业知识库 MCP 服务',
    'builtin',
    'http',
    '/mcp',
    'active'
) ON CONFLICT (id) DO NOTHING;

-- 2. 身份凭证库 (Credential)
CREATE TABLE IF NOT EXISTS credentials (
    id                      TEXT PRIMARY KEY,
    owner_type              TEXT NOT NULL, -- USER | ORGANIZATION | SERVICE_ACCOUNT
    owner_id                TEXT NOT NULL DEFAULT '',
    provider                TEXT NOT NULL DEFAULT 'custom', -- feishu | github | jira | gitlab | custom
    auth_type               TEXT NOT NULL DEFAULT 'api_key', -- oauth | api_key | bearer | pat | basic
    credential_name         TEXT NOT NULL,
    secret_ref              TEXT NOT NULL DEFAULT '', -- 关联 Vault / SecretManager 密钥引用 ID
    masked_value            TEXT NOT NULL DEFAULT '***',
    refresh_token_encrypted TEXT NOT NULL DEFAULT '',
    expires_at              TIMESTAMPTZ,
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb,
    status                  TEXT NOT NULL DEFAULT 'active', -- active | expired | revoked
    created_by              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_credentials_owner ON credentials(owner_type, owner_id);
CREATE INDEX IF NOT EXISTS idx_credentials_provider ON credentials(provider);

-- 3. 数字员工与 MCP 绑定表 (EmployeeMCPBinding)
CREATE TABLE IF NOT EXISTS employee_mcp_bindings (
    id              TEXT PRIMARY KEY,
    employee_id     TEXT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    mcp_server_id   TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    credential_id   TEXT REFERENCES credentials(id) ON DELETE SET NULL,
    enabled         BOOLEAN NOT NULL DEFAULT TRUE,
    allowed_tools   JSONB NOT NULL DEFAULT '[]'::jsonb,
    denied_tools    JSONB NOT NULL DEFAULT '[]'::jsonb,
    config          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(employee_id, mcp_server_id)
);

CREATE INDEX IF NOT EXISTS idx_employee_mcp_bindings_emp ON employee_mcp_bindings(employee_id);
CREATE INDEX IF NOT EXISTS idx_employee_mcp_bindings_mcp ON employee_mcp_bindings(mcp_server_id);

-- 为当前已有数字员工默认开启内置 workflow-mcp 绑定，确保向下兼容
INSERT INTO employee_mcp_bindings (id, employee_id, mcp_server_id, enabled)
SELECT 'bnd-wf-' || id, id, 'mcp-workflow', TRUE
FROM employees
ON CONFLICT (employee_id, mcp_server_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS employee_mcp_bindings;
DROP TABLE IF EXISTS credentials;
DROP TABLE IF EXISTS mcp_servers;
