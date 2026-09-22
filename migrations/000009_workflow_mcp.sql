-- +goose Up
-- 工作流MCP：工作流 / 技能包 / 知识库 / 员工授权 / MCP Token
-- 设计依据：docs/WORKFLOW_MCP.md；参考 PersonalWorkMCP，权限按「工作流单点授权」重写

-- 删除 000002 遗留的空表（Go 层从未写入）
DROP TABLE IF EXISTS employee_knowledge;
DROP TABLE IF EXISTS employee_skills;
DROP TABLE IF EXISTS knowledge;
DROP TABLE IF EXISTS skills;

-- 工作流（权限主体）
CREATE TABLE IF NOT EXISTS workflows (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    version         TEXT NOT NULL DEFAULT '1.0.0',
    description     TEXT NOT NULL DEFAULT '',
    when_to_use     JSONB NOT NULL DEFAULT '[]'::jsonb,
    inputs          JSONB NOT NULL DEFAULT '[]'::jsonb,
    skill_refs      JSONB NOT NULL DEFAULT '[]'::jsonb,
    knowledge_refs  JSONB NOT NULL DEFAULT '[]'::jsonb,
    steps           JSONB NOT NULL DEFAULT '[]'::jsonb,
    approval        JSONB NOT NULL DEFAULT '{}'::jsonb,
    extra           JSONB NOT NULL DEFAULT '{}'::jsonb,
    status          TEXT NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status);

-- 技能包（Cursor 兼容 SKILL.md 包）
CREATE TABLE IF NOT EXISTS skill_packages (
    id                        TEXT PRIMARY KEY,
    cursor_name               TEXT NOT NULL UNIQUE,
    name                      TEXT NOT NULL DEFAULT '',
    version                   TEXT NOT NULL DEFAULT '1.0.0',
    description               TEXT NOT NULL DEFAULT '',
    body                      TEXT NOT NULL DEFAULT '',
    metadata                  JSONB NOT NULL DEFAULT '{}'::jsonb,
    disable_model_invocation  BOOLEAN NOT NULL DEFAULT TRUE,
    content_hash              TEXT NOT NULL DEFAULT '',
    created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS skill_package_files (
    skill_id    TEXT NOT NULL REFERENCES skill_packages(id) ON DELETE CASCADE,
    path        TEXT NOT NULL,
    content     BYTEA NOT NULL,
    sha256      TEXT NOT NULL DEFAULT '',
    size_bytes  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (skill_id, path)
);

-- 知识文档
CREATE TABLE IF NOT EXISTS knowledge_docs (
    id          TEXT PRIMARY KEY,
    path        TEXT NOT NULL UNIQUE,
    namespace   TEXT NOT NULL DEFAULT '',
    title       TEXT NOT NULL DEFAULT '',
    source      TEXT NOT NULL DEFAULT 'local',
    content     TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    aliases     JSONB NOT NULL DEFAULT '[]'::jsonb,
    version     TEXT NOT NULL DEFAULT '1.0.0',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_knowledge_docs_namespace ON knowledge_docs(namespace);

-- 知识分块（自定义中英文分词 + tsvector，不依赖 zhparser）
CREATE TABLE IF NOT EXISTS knowledge_chunks (
    id          BIGSERIAL PRIMARY KEY,
    doc_id      TEXT NOT NULL REFERENCES knowledge_docs(id) ON DELETE CASCADE,
    chunk_index INT NOT NULL,
    heading     TEXT NOT NULL DEFAULT '',
    content     TEXT NOT NULL DEFAULT '',
    tokens      TEXT NOT NULL DEFAULT '',
    tsv         TSVECTOR,
    UNIQUE (doc_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_doc ON knowledge_chunks(doc_id);
CREATE INDEX IF NOT EXISTS idx_knowledge_chunks_tsv ON knowledge_chunks USING GIN(tsv);

-- 员工 ↔ 工作流授权（唯一的员工侧绑定）
CREATE TABLE IF NOT EXISTS employee_workflows (
    employee_id  TEXT NOT NULL REFERENCES employees(id) ON DELETE CASCADE,
    workflow_id  TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    granted_by   TEXT NOT NULL DEFAULT '',
    granted_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (employee_id, workflow_id)
);

CREATE INDEX IF NOT EXISTS idx_employee_workflows_wf ON employee_workflows(workflow_id);

-- MCP Token（aiemcp_ 前缀）
CREATE TABLE IF NOT EXISTS mcp_tokens (
    id            TEXT PRIMARY KEY,
    token_hash    TEXT NOT NULL UNIQUE,
    subject_type  TEXT NOT NULL,
    subject_id    TEXT NOT NULL,
    scope         TEXT NOT NULL DEFAULT 'READ',
    label         TEXT NOT NULL DEFAULT '',
    expires_at    TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ,
    created_by    TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcp_tokens_subject ON mcp_tokens(subject_type, subject_id);

-- Job 关联工作流快照
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS workflow_id TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS workflow_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

-- 权限码种子
INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000020', 'workflow.read', '读取工作流/技能/知识'),
    ('00000000-0000-0000-0001-000000000021', 'workflow.write', '写入工作流/技能/知识'),
    ('00000000-0000-0000-0001-000000000022', 'workflow.delete', '删除工作流/技能/知识'),
    ('00000000-0000-0000-0001-000000000023', 'workflow.grant', '向员工授权工作流并签发 MCP Token')
ON CONFLICT (code) DO NOTHING;

-- ADMIN / OPERATOR / VIEWER 角色赋权
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'ADMIN'
  AND p.code IN ('workflow.read', 'workflow.write', 'workflow.delete', 'workflow.grant')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'OPERATOR'
  AND p.code IN ('workflow.read', 'workflow.grant')
ON CONFLICT DO NOTHING;

INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r
CROSS JOIN permissions p
WHERE r.name = 'VIEWER'
  AND p.code = 'workflow.read'
ON CONFLICT DO NOTHING;

-- +goose Down
ALTER TABLE jobs DROP COLUMN IF EXISTS workflow_snapshot;
ALTER TABLE jobs DROP COLUMN IF EXISTS workflow_id;

DELETE FROM role_permissions WHERE permission_id IN (
    SELECT id FROM permissions WHERE code LIKE 'workflow.%'
);
DELETE FROM permissions WHERE code LIKE 'workflow.%';

DROP TABLE IF EXISTS mcp_tokens;
DROP TABLE IF EXISTS employee_workflows;
DROP TABLE IF EXISTS knowledge_chunks;
DROP TABLE IF EXISTS knowledge_docs;
DROP TABLE IF EXISTS skill_package_files;
DROP TABLE IF EXISTS skill_packages;
DROP TABLE IF EXISTS workflows;
