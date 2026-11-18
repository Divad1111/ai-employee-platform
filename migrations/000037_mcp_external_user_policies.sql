-- +goose Up
CREATE TABLE mcp_external_user_policies (
    user_id UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    can_write BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE mcp_external_user_policies;
