-- +goose Up
-- users / roles / permissions（RBAC）
-- 设计依据：§26、§27、§78、§79

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    password_hash   TEXT NOT NULL,
    display_name    TEXT NOT NULL DEFAULT '',
    failed_attempts INT NOT NULL DEFAULT 0,
    locked_until    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS roles (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS permissions (
    id          UUID PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS user_roles (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS role_permissions (
    role_id       UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES permissions(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, permission_id)
);

-- 种子角色
INSERT INTO roles (id, name, description) VALUES
    ('00000000-0000-0000-0000-000000000001', 'SUPER_ADMIN', '超级管理员'),
    ('00000000-0000-0000-0000-000000000002', 'ADMIN', '管理员'),
    ('00000000-0000-0000-0000-000000000003', 'OPERATOR', '操作员'),
    ('00000000-0000-0000-0000-000000000004', 'VIEWER', '只读')
ON CONFLICT (name) DO NOTHING;

-- 种子权限（对齐 §27 示例集）
INSERT INTO permissions (id, code, description) VALUES
    ('00000000-0000-0000-0001-000000000001', 'employee.read', '读取 Employee'),
    ('00000000-0000-0000-0001-000000000002', 'employee.write', '写入 Employee'),
    ('00000000-0000-0000-0001-000000000003', 'employee.delete', '删除 Employee'),
    ('00000000-0000-0000-0001-000000000004', 'workstation.read', '读取 Workstation'),
    ('00000000-0000-0000-0001-000000000005', 'workstation.write', '写入 Workstation'),
    ('00000000-0000-0000-0001-000000000006', 'job.read', '读取 Job'),
    ('00000000-0000-0000-0001-000000000007', 'job.cancel', '取消 Job'),
    ('00000000-0000-0000-0001-000000000008', 'approval.read', '读取审批'),
    ('00000000-0000-0000-0001-000000000009', 'approval.approve', '批准审批'),
    ('00000000-0000-0000-0001-00000000000a', 'secret.read', '读取 Secret 元数据'),
    ('00000000-0000-0000-0001-00000000000b', 'secret.write', '写入 Secret'),
    ('00000000-0000-0000-0001-00000000000c', 'system.read', '读取系统配置'),
    ('00000000-0000-0000-0001-00000000000d', 'system.write', '写入系统配置')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS user_roles;
DROP TABLE IF EXISTS permissions;
DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS users;
