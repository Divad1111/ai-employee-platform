package mcp

import "time"

const (
	ServerTypeBuiltin = "builtin"
	ServerTypeCustom  = "custom"

	TransportHTTP  = "http"
	TransportSSE   = "sse"
	TransportStdio = "stdio"

	OwnerTypeUser           = "USER"
	OwnerTypeOrganization   = "ORGANIZATION"
	OwnerTypeServiceAccount = "SERVICE_ACCOUNT"

	AuthTypeOAuth  = "oauth"
	AuthTypeAPIKey = "api_key"
	AuthTypeBearer = "bearer"
	AuthTypePAT    = "pat"
	AuthTypeBasic  = "basic"

	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusRevoked  = "revoked"
	StatusExpired  = "expired"
)

// MCPServer 代表外部或内置的 MCP 能力提供方（规范 §5）。
type MCPServer struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	ServerType  string         `json:"server_type"` // builtin | custom
	Transport   string         `json:"transport"`   // http | sse | stdio
	Endpoint    string         `json:"endpoint"`    // url or command
	Config      map[string]any `json:"config"`      // headers, env, args, etc.
	Status      string         `json:"status"`      // active | disabled
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`

	BoundEmployeeCount int `json:"bound_employee_count,omitempty"`
}

// Credential 代表访问外部系统的身份凭证（规范 §6）。
type Credential struct {
	ID                    string         `json:"id"`
	OwnerType             string         `json:"owner_type"` // USER | ORGANIZATION | SERVICE_ACCOUNT
	OwnerID               string         `json:"owner_id"`
	Provider              string         `json:"provider"`  // feishu | github | jira | custom
	AuthType              string         `json:"auth_type"` // oauth | api_key | bearer | pat | basic
	CredentialName        string         `json:"credential_name"`
	SecretRef             string         `json:"secret_ref,omitempty"`
	MaskedValue           string         `json:"masked_value"`
	RefreshTokenEncrypted string         `json:"-"`
	ExpiresAt             *time.Time     `json:"expires_at,omitempty"`
	Metadata              map[string]any `json:"metadata"`
	Status                string         `json:"status"` // active | expired | revoked
	CreatedBy             string         `json:"created_by,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"`

	BoundEmployeeCount int `json:"bound_employee_count,omitempty"`
}

// EmployeeMCPBinding 代表数字员工与 MCP Server 及 Credential 的绑定（规范 §7）。
type EmployeeMCPBinding struct {
	ID           string         `json:"id"`
	EmployeeID   string         `json:"employee_id"`
	MCPServerID  string         `json:"mcp_server_id"`
	CredentialID string         `json:"credential_id,omitempty"`
	Enabled      bool           `json:"enabled"`
	AllowedTools []string       `json:"allowed_tools"`
	DeniedTools  []string       `json:"denied_tools"`
	Config       map[string]any `json:"config"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`

	// 联合附加字段（方便前端展示）
	MCPServerName      string `json:"mcp_server_name,omitempty"`
	MCPServerType      string `json:"mcp_server_type,omitempty"`
	MCPTransport       string `json:"mcp_transport,omitempty"`
	CredentialName     string `json:"credential_name,omitempty"`
	CredentialProvider string `json:"credential_provider,omitempty"`
	CredentialMasked   string `json:"credential_masked,omitempty"`
}
