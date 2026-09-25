package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type pgStore struct {
	db *sql.DB
}

// NewPGStore 创建 PostgreSQL 存储实现。
func NewPGStore(db *sql.DB) Store {
	return &pgStore{db: db}
}

// ---------- MCP Servers ----------

func (s *pgStore) ListServers(ctx context.Context) ([]*MCPServer, error) {
	query := `
		SELECT s.id, s.name, s.description, s.server_type, s.transport, s.endpoint, s.config, s.status, s.created_at, s.updated_at,
		       COUNT(b.id) FILTER (WHERE b.enabled = TRUE) as bound_count
		FROM mcp_servers s
		LEFT JOIN employee_mcp_bindings b ON s.id = b.mcp_server_id
		GROUP BY s.id
		ORDER BY CASE WHEN s.server_type = 'builtin' THEN 0 ELSE 1 END, s.created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*MCPServer
	for rows.Next() {
		var (
			srv       MCPServer
			configRaw []byte
		)
		if err := rows.Scan(
			&srv.ID, &srv.Name, &srv.Description, &srv.ServerType, &srv.Transport,
			&srv.Endpoint, &configRaw, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt,
			&srv.BoundEmployeeCount,
		); err != nil {
			return nil, err
		}
		if len(configRaw) > 0 {
			_ = json.Unmarshal(configRaw, &srv.Config)
		}
		if srv.Config == nil {
			srv.Config = map[string]any{}
		}
		out = append(out, &srv)
	}
	return out, rows.Err()
}

func (s *pgStore) GetServer(ctx context.Context, id string) (*MCPServer, error) {
	query := `
		SELECT s.id, s.name, s.description, s.server_type, s.transport, s.endpoint, s.config, s.status, s.created_at, s.updated_at,
		       COUNT(b.id) FILTER (WHERE b.enabled = TRUE) as bound_count
		FROM mcp_servers s
		LEFT JOIN employee_mcp_bindings b ON s.id = b.mcp_server_id
		WHERE s.id = $1
		GROUP BY s.id
	`
	var (
		srv       MCPServer
		configRaw []byte
	)
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&srv.ID, &srv.Name, &srv.Description, &srv.ServerType, &srv.Transport,
		&srv.Endpoint, &configRaw, &srv.Status, &srv.CreatedAt, &srv.UpdatedAt,
		&srv.BoundEmployeeCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrServerNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(configRaw) > 0 {
		_ = json.Unmarshal(configRaw, &srv.Config)
	}
	if srv.Config == nil {
		srv.Config = map[string]any{}
	}
	return &srv, nil
}

func (s *pgStore) CreateServer(ctx context.Context, srv *MCPServer) error {
	configRaw, _ := json.Marshal(srv.Config)
	if srv.Config == nil {
		configRaw = []byte("{}")
	}
	query := `
		INSERT INTO mcp_servers (id, name, description, server_type, transport, endpoint, config, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
	`
	_, err := s.db.ExecContext(ctx, query,
		srv.ID, srv.Name, srv.Description, srv.ServerType, srv.Transport,
		srv.Endpoint, configRaw, srv.Status,
	)
	return err
}

func (s *pgStore) UpdateServer(ctx context.Context, srv *MCPServer) error {
	configRaw, _ := json.Marshal(srv.Config)
	if srv.Config == nil {
		configRaw = []byte("{}")
	}
	query := `
		UPDATE mcp_servers
		SET name = $2, description = $3, transport = $4, endpoint = $5, config = $6, status = $7, updated_at = NOW()
		WHERE id = $1
	`
	res, err := s.db.ExecContext(ctx, query,
		srv.ID, srv.Name, srv.Description, srv.Transport, srv.Endpoint, configRaw, srv.Status,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrServerNotFound
	}
	return nil
}

func (s *pgStore) DeleteServer(ctx context.Context, id string) error {
	srv, err := s.GetServer(ctx, id)
	if err != nil {
		return err
	}
	if srv.ServerType == ServerTypeBuiltin {
		return ErrCannotDeleteBuiltin
	}
	query := `DELETE FROM mcp_servers WHERE id = $1`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrServerNotFound
	}
	return nil
}

// ---------- Credentials ----------

func (s *pgStore) ListCredentials(ctx context.Context, ownerType, ownerID string) ([]*Credential, error) {
	query := `
		SELECT c.id, c.owner_type, c.owner_id, c.provider, c.auth_type, c.credential_name,
		       c.secret_ref, c.masked_value, c.refresh_token_encrypted, c.expires_at, c.metadata,
		       c.status, c.created_by, c.created_at, c.updated_at,
		       COUNT(b.id) as bound_count
		FROM credentials c
		LEFT JOIN employee_mcp_bindings b ON c.id = b.credential_id
		WHERE ($1 = '' OR c.owner_type = $1)
		  AND ($2 = '' OR c.owner_id = $2)
		GROUP BY c.id
		ORDER BY c.created_at DESC
	`
	rows, err := s.db.QueryContext(ctx, query, ownerType, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Credential
	for rows.Next() {
		var (
			c       Credential
			metaRaw []byte
		)
		if err := rows.Scan(
			&c.ID, &c.OwnerType, &c.OwnerID, &c.Provider, &c.AuthType, &c.CredentialName,
			&c.SecretRef, &c.MaskedValue, &c.RefreshTokenEncrypted, &c.ExpiresAt, &metaRaw,
			&c.Status, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
			&c.BoundEmployeeCount,
		); err != nil {
			return nil, err
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &c.Metadata)
		}
		if c.Metadata == nil {
			c.Metadata = map[string]any{}
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

func (s *pgStore) GetCredential(ctx context.Context, id string) (*Credential, error) {
	query := `
		SELECT c.id, c.owner_type, c.owner_id, c.provider, c.auth_type, c.credential_name,
		       c.secret_ref, c.masked_value, c.refresh_token_encrypted, c.expires_at, c.metadata,
		       c.status, c.created_by, c.created_at, c.updated_at,
		       COUNT(b.id) as bound_count
		FROM credentials c
		LEFT JOIN employee_mcp_bindings b ON c.id = b.credential_id
		WHERE c.id = $1
		GROUP BY c.id
	`
	var (
		c       Credential
		metaRaw []byte
	)
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.OwnerType, &c.OwnerID, &c.Provider, &c.AuthType, &c.CredentialName,
		&c.SecretRef, &c.MaskedValue, &c.RefreshTokenEncrypted, &c.ExpiresAt, &metaRaw,
		&c.Status, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
		&c.BoundEmployeeCount,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCredentialNotFound
	}
	if err != nil {
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &c.Metadata)
	}
	if c.Metadata == nil {
		c.Metadata = map[string]any{}
	}
	return &c, nil
}

func (s *pgStore) CreateCredential(ctx context.Context, c *Credential) error {
	metaRaw, _ := json.Marshal(c.Metadata)
	if c.Metadata == nil {
		metaRaw = []byte("{}")
	}
	query := `
		INSERT INTO credentials (
			id, owner_type, owner_id, provider, auth_type, credential_name,
			secret_ref, masked_value, refresh_token_encrypted, expires_at, metadata,
			status, created_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
	`
	_, err := s.db.ExecContext(ctx, query,
		c.ID, c.OwnerType, c.OwnerID, c.Provider, c.AuthType, c.CredentialName,
		c.SecretRef, c.MaskedValue, c.RefreshTokenEncrypted, c.ExpiresAt, metaRaw,
		c.Status, c.CreatedBy,
	)
	return err
}

func (s *pgStore) UpdateCredential(ctx context.Context, c *Credential) error {
	metaRaw, _ := json.Marshal(c.Metadata)
	if c.Metadata == nil {
		metaRaw = []byte("{}")
	}
	query := `
		UPDATE credentials
		SET credential_name = $2, provider = $3, auth_type = $4, secret_ref = $5,
		    masked_value = $6, expires_at = $7, metadata = $8, status = $9, updated_at = NOW()
		WHERE id = $1
	`
	res, err := s.db.ExecContext(ctx, query,
		c.ID, c.CredentialName, c.Provider, c.AuthType, c.SecretRef,
		c.MaskedValue, c.ExpiresAt, metaRaw, c.Status,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

func (s *pgStore) DeleteCredential(ctx context.Context, id string) error {
	query := `DELETE FROM credentials WHERE id = $1`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrCredentialNotFound
	}
	return nil
}

// ---------- Employee MCP Bindings ----------

func (s *pgStore) ListBindingsByEmployee(ctx context.Context, employeeID string) ([]*EmployeeMCPBinding, error) {
	query := `
		SELECT b.id, b.employee_id, b.mcp_server_id, b.credential_id, b.enabled,
		       b.allowed_tools, b.denied_tools, b.config, b.created_at, b.updated_at,
		       s.name, s.server_type, s.transport,
		       COALESCE(c.credential_name, ''), COALESCE(c.provider, ''), COALESCE(c.masked_value, '')
		FROM employee_mcp_bindings b
		JOIN mcp_servers s ON b.mcp_server_id = s.id
		LEFT JOIN credentials c ON b.credential_id = c.id
		WHERE b.employee_id = $1
		ORDER BY CASE WHEN s.server_type = 'builtin' THEN 0 ELSE 1 END, b.created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*EmployeeMCPBinding
	for rows.Next() {
		var (
			b                EmployeeMCPBinding
			allowedRaw       []byte
			deniedRaw        []byte
			cfgRaw           []byte
			credID           sql.NullString
		)
		if err := rows.Scan(
			&b.ID, &b.EmployeeID, &b.MCPServerID, &credID, &b.Enabled,
			&allowedRaw, &deniedRaw, &cfgRaw, &b.CreatedAt, &b.UpdatedAt,
			&b.MCPServerName, &b.MCPServerType, &b.MCPTransport,
			&b.CredentialName, &b.CredentialProvider, &b.CredentialMasked,
		); err != nil {
			return nil, err
		}
		if credID.Valid {
			b.CredentialID = credID.String
		}
		if len(allowedRaw) > 0 {
			_ = json.Unmarshal(allowedRaw, &b.AllowedTools)
		}
		if len(deniedRaw) > 0 {
			_ = json.Unmarshal(deniedRaw, &b.DeniedTools)
		}
		if len(cfgRaw) > 0 {
			_ = json.Unmarshal(cfgRaw, &b.Config)
		}
		if b.AllowedTools == nil {
			b.AllowedTools = []string{}
		}
		if b.DeniedTools == nil {
			b.DeniedTools = []string{}
		}
		if b.Config == nil {
			b.Config = map[string]any{}
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

func (s *pgStore) ListBindingsByServer(ctx context.Context, serverID string) ([]*EmployeeMCPBinding, error) {
	query := `
		SELECT b.id, b.employee_id, b.mcp_server_id, b.credential_id, b.enabled,
		       b.allowed_tools, b.denied_tools, b.config, b.created_at, b.updated_at,
		       s.name, s.server_type, s.transport,
		       COALESCE(c.credential_name, ''), COALESCE(c.provider, ''), COALESCE(c.masked_value, '')
		FROM employee_mcp_bindings b
		JOIN mcp_servers s ON b.mcp_server_id = s.id
		LEFT JOIN credentials c ON b.credential_id = c.id
		WHERE b.mcp_server_id = $1
		ORDER BY b.created_at ASC
	`
	rows, err := s.db.QueryContext(ctx, query, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*EmployeeMCPBinding
	for rows.Next() {
		var (
			b          EmployeeMCPBinding
			allowedRaw []byte
			deniedRaw  []byte
			cfgRaw     []byte
			credID     sql.NullString
		)
		if err := rows.Scan(
			&b.ID, &b.EmployeeID, &b.MCPServerID, &credID, &b.Enabled,
			&allowedRaw, &deniedRaw, &cfgRaw, &b.CreatedAt, &b.UpdatedAt,
			&b.MCPServerName, &b.MCPServerType, &b.MCPTransport,
			&b.CredentialName, &b.CredentialProvider, &b.CredentialMasked,
		); err != nil {
			return nil, err
		}
		if credID.Valid {
			b.CredentialID = credID.String
		}
		if len(allowedRaw) > 0 {
			_ = json.Unmarshal(allowedRaw, &b.AllowedTools)
		}
		if len(deniedRaw) > 0 {
			_ = json.Unmarshal(deniedRaw, &b.DeniedTools)
		}
		if len(cfgRaw) > 0 {
			_ = json.Unmarshal(cfgRaw, &b.Config)
		}
		if b.AllowedTools == nil {
			b.AllowedTools = []string{}
		}
		if b.DeniedTools == nil {
			b.DeniedTools = []string{}
		}
		if b.Config == nil {
			b.Config = map[string]any{}
		}
		out = append(out, &b)
	}
	return out, rows.Err()
}

func (s *pgStore) GetBinding(ctx context.Context, id string) (*EmployeeMCPBinding, error) {
	query := `
		SELECT b.id, b.employee_id, b.mcp_server_id, b.credential_id, b.enabled,
		       b.allowed_tools, b.denied_tools, b.config, b.created_at, b.updated_at,
		       s.name, s.server_type, s.transport,
		       COALESCE(c.credential_name, ''), COALESCE(c.provider, ''), COALESCE(c.masked_value, '')
		FROM employee_mcp_bindings b
		JOIN mcp_servers s ON b.mcp_server_id = s.id
		LEFT JOIN credentials c ON b.credential_id = c.id
		WHERE b.id = $1
	`
	var (
		b          EmployeeMCPBinding
		allowedRaw []byte
		deniedRaw  []byte
		cfgRaw     []byte
		credID     sql.NullString
	)
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&b.ID, &b.EmployeeID, &b.MCPServerID, &credID, &b.Enabled,
		&allowedRaw, &deniedRaw, &cfgRaw, &b.CreatedAt, &b.UpdatedAt,
		&b.MCPServerName, &b.MCPServerType, &b.MCPTransport,
		&b.CredentialName, &b.CredentialProvider, &b.CredentialMasked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	if credID.Valid {
		b.CredentialID = credID.String
	}
	if len(allowedRaw) > 0 {
		_ = json.Unmarshal(allowedRaw, &b.AllowedTools)
	}
	if len(deniedRaw) > 0 {
		_ = json.Unmarshal(deniedRaw, &b.DeniedTools)
	}
	if len(cfgRaw) > 0 {
		_ = json.Unmarshal(cfgRaw, &b.Config)
	}
	if b.AllowedTools == nil {
		b.AllowedTools = []string{}
	}
	if b.DeniedTools == nil {
		b.DeniedTools = []string{}
	}
	if b.Config == nil {
		b.Config = map[string]any{}
	}
	return &b, nil
}

func (s *pgStore) GetBindingByEmployeeAndServer(ctx context.Context, employeeID, serverID string) (*EmployeeMCPBinding, error) {
	query := `
		SELECT b.id, b.employee_id, b.mcp_server_id, b.credential_id, b.enabled,
		       b.allowed_tools, b.denied_tools, b.config, b.created_at, b.updated_at,
		       s.name, s.server_type, s.transport,
		       COALESCE(c.credential_name, ''), COALESCE(c.provider, ''), COALESCE(c.masked_value, '')
		FROM employee_mcp_bindings b
		JOIN mcp_servers s ON b.mcp_server_id = s.id
		LEFT JOIN credentials c ON b.credential_id = c.id
		WHERE b.employee_id = $1 AND b.mcp_server_id = $2
	`
	var (
		b          EmployeeMCPBinding
		allowedRaw []byte
		deniedRaw  []byte
		cfgRaw     []byte
		credID     sql.NullString
	)
	err := s.db.QueryRowContext(ctx, query, employeeID, serverID).Scan(
		&b.ID, &b.EmployeeID, &b.MCPServerID, &credID, &b.Enabled,
		&allowedRaw, &deniedRaw, &cfgRaw, &b.CreatedAt, &b.UpdatedAt,
		&b.MCPServerName, &b.MCPServerType, &b.MCPTransport,
		&b.CredentialName, &b.CredentialProvider, &b.CredentialMasked,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrBindingNotFound
	}
	if err != nil {
		return nil, err
	}
	if credID.Valid {
		b.CredentialID = credID.String
	}
	if len(allowedRaw) > 0 {
		_ = json.Unmarshal(allowedRaw, &b.AllowedTools)
	}
	if len(deniedRaw) > 0 {
		_ = json.Unmarshal(deniedRaw, &b.DeniedTools)
	}
	if len(cfgRaw) > 0 {
		_ = json.Unmarshal(cfgRaw, &b.Config)
	}
	if b.AllowedTools == nil {
		b.AllowedTools = []string{}
	}
	if b.DeniedTools == nil {
		b.DeniedTools = []string{}
	}
	if b.Config == nil {
		b.Config = map[string]any{}
	}
	return &b, nil
}

func (s *pgStore) CreateBinding(ctx context.Context, b *EmployeeMCPBinding) error {
	allowedRaw, _ := json.Marshal(b.AllowedTools)
	deniedRaw, _ := json.Marshal(b.DeniedTools)
	cfgRaw, _ := json.Marshal(b.Config)
	var cred sql.NullString
	if b.CredentialID != "" {
		cred = sql.NullString{String: b.CredentialID, Valid: true}
	}
	query := `
		INSERT INTO employee_mcp_bindings (
			id, employee_id, mcp_server_id, credential_id, enabled,
			allowed_tools, denied_tools, config, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
	`
	_, err := s.db.ExecContext(ctx, query,
		b.ID, b.EmployeeID, b.MCPServerID, cred, b.Enabled,
		allowedRaw, deniedRaw, cfgRaw,
	)
	return err
}

func (s *pgStore) UpdateBinding(ctx context.Context, b *EmployeeMCPBinding) error {
	allowedRaw, _ := json.Marshal(b.AllowedTools)
	deniedRaw, _ := json.Marshal(b.DeniedTools)
	cfgRaw, _ := json.Marshal(b.Config)
	var cred sql.NullString
	if b.CredentialID != "" {
		cred = sql.NullString{String: b.CredentialID, Valid: true}
	}
	query := `
		UPDATE employee_mcp_bindings
		SET credential_id = $2, enabled = $3, allowed_tools = $4, denied_tools = $5,
		    config = $6, updated_at = NOW()
		WHERE id = $1
	`
	res, err := s.db.ExecContext(ctx, query,
		b.ID, cred, b.Enabled, allowedRaw, deniedRaw, cfgRaw,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrBindingNotFound
	}
	return nil
}

func (s *pgStore) DeleteBinding(ctx context.Context, id string) error {
	query := `DELETE FROM employee_mcp_bindings WHERE id = $1`
	res, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrBindingNotFound
	}
	return nil
}
