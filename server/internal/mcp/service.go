package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
	"github.com/ai-employee-platform/server/internal/secret"
)

// Service 协调 MCP Server、Credential 与 Employee 绑定的领域服务。
type Service struct {
	store     Store
	secretMgr *secret.Manager
}

// NewService 创建 MCP 业务服务。
func NewService(store Store, secretMgr *secret.Manager) *Service {
	return &Service{
		store:     store,
		secretMgr: secretMgr,
	}
}

// Store 导出存储实例（用于测试或高级查询）。
func (s *Service) Store() Store {
	return s.store
}

// ---------- MCP Server 业务逻辑 ----------

type CreateServerInput struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Transport   string         `json:"transport"` // http | sse | stdio
	Endpoint    string         `json:"endpoint"`  // url or command
	Config      map[string]any `json:"config"`
	Status      string         `json:"status"`
}

func (s *Service) ListServers(ctx context.Context) ([]*MCPServer, error) {
	return s.store.ListServers(ctx)
}

func (s *Service) GetServer(ctx context.Context, id string) (*MCPServer, error) {
	return s.store.GetServer(ctx, id)
}

func (s *Service) CreateServer(ctx context.Context, in CreateServerInput) (*MCPServer, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errors.New("MCP 服务名称不能为空")
	}
	transport := strings.ToLower(strings.TrimSpace(in.Transport))
	if transport == "" {
		transport = TransportHTTP
	}
	if transport != TransportHTTP && transport != TransportSSE && transport != TransportStdio {
		return nil, fmt.Errorf("不支持的传输协议类型: %s", transport)
	}

	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = "mcp-" + randomHex(4)
	}
	status := strings.ToLower(strings.TrimSpace(in.Status))
	if status == "" {
		status = StatusActive
	}

	srv := &MCPServer{
		ID:          id,
		Name:        name,
		Description: strings.TrimSpace(in.Description),
		ServerType:  ServerTypeCustom,
		Transport:   transport,
		Endpoint:    strings.TrimSpace(in.Endpoint),
		Config:      in.Config,
		Status:      status,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := s.store.CreateServer(ctx, srv); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Service) UpdateServer(ctx context.Context, id string, in CreateServerInput) (*MCPServer, error) {
	srv, err := s.store.GetServer(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Name != "" {
		srv.Name = strings.TrimSpace(in.Name)
	}
	srv.Description = strings.TrimSpace(in.Description)
	if in.Transport != "" {
		srv.Transport = strings.ToLower(strings.TrimSpace(in.Transport))
	}
	srv.Endpoint = strings.TrimSpace(in.Endpoint)
	if in.Config != nil {
		srv.Config = in.Config
	}
	if in.Status != "" {
		srv.Status = strings.ToLower(strings.TrimSpace(in.Status))
	}
	srv.UpdatedAt = time.Now()

	if err := s.store.UpdateServer(ctx, srv); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Service) DeleteServer(ctx context.Context, id string) error {
	return s.store.DeleteServer(ctx, id)
}

// ---------- Credential 业务逻辑 ----------

type CreateCredentialInput struct {
	OwnerType      string         `json:"owner_type"` // USER | ORGANIZATION | SERVICE_ACCOUNT
	OwnerID        string         `json:"owner_id"`
	Provider       string         `json:"provider"`  // feishu | github | jira | custom
	AuthType       string         `json:"auth_type"` // oauth | api_key | bearer | pat | basic
	CredentialName string         `json:"credential_name"`
	SecretValue    string         `json:"secret_value"` // 明文，仅用于加密后存入 Vault，不落库
	ExpiresAt      *time.Time     `json:"expires_at"`
	Metadata       map[string]any `json:"metadata"`
	CreatedBy      string         `json:"created_by"`
}

func (s *Service) ListCredentials(ctx context.Context, ownerType, ownerID string) ([]*Credential, error) {
	return s.store.ListCredentials(ctx, ownerType, ownerID)
}

func (s *Service) GetCredential(ctx context.Context, id string) (*Credential, error) {
	return s.store.GetCredential(ctx, id)
}

func (s *Service) CreateCredential(ctx context.Context, in CreateCredentialInput) (*Credential, error) {
	name := strings.TrimSpace(in.CredentialName)
	if name == "" {
		return nil, errors.New("凭证名称不能为空")
	}
	ownerType := strings.ToUpper(strings.TrimSpace(in.OwnerType))
	if ownerType == "" {
		ownerType = OwnerTypeUser
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider == "" {
		provider = "custom"
	}
	authType := strings.ToLower(strings.TrimSpace(in.AuthType))
	if authType == "" {
		authType = AuthTypeAPIKey
	}

	secretRef := ""
	masked := "***"
	val := strings.TrimSpace(in.SecretValue)
	if val != "" {
		masked = maskSecret(val)
		if s.secretMgr != nil {
			meta, err := s.secretMgr.Put(ctx, name, val, in.CreatedBy, "", fmt.Sprintf("MCP 凭证 [%s]", name))
			if err == nil && meta.ID != "" {
				secretRef = meta.ID
			}
		}
	}

	id := idgen.New("cred")
	cred := &Credential{
		ID:             id,
		OwnerType:      ownerType,
		OwnerID:        strings.TrimSpace(in.OwnerID),
		Provider:       provider,
		AuthType:       authType,
		CredentialName: name,
		SecretRef:      secretRef,
		MaskedValue:    masked,
		ExpiresAt:      in.ExpiresAt,
		Metadata:       in.Metadata,
		Status:         StatusActive,
		CreatedBy:      in.CreatedBy,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := s.store.CreateCredential(ctx, cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func (s *Service) UpdateCredential(ctx context.Context, id string, in CreateCredentialInput) (*Credential, error) {
	cred, err := s.store.GetCredential(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.CredentialName != "" {
		cred.CredentialName = strings.TrimSpace(in.CredentialName)
	}
	if in.Provider != "" {
		cred.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	}
	if in.AuthType != "" {
		cred.AuthType = strings.ToLower(strings.TrimSpace(in.AuthType))
	}
	if in.Metadata != nil {
		cred.Metadata = in.Metadata
	}
	if in.ExpiresAt != nil {
		cred.ExpiresAt = in.ExpiresAt
	}
	if val := strings.TrimSpace(in.SecretValue); val != "" {
		cred.MaskedValue = maskSecret(val)
		if s.secretMgr != nil {
			if cred.SecretRef != "" {
				_, _ = s.secretMgr.Rotate(ctx, cred.SecretRef, val, in.CreatedBy, "")
			} else {
				meta, err := s.secretMgr.Put(ctx, cred.CredentialName, val, in.CreatedBy, "", "MCP 凭证")
				if err == nil && meta.ID != "" {
					cred.SecretRef = meta.ID
				}
			}
		}
	}
	cred.UpdatedAt = time.Now()
	if err := s.store.UpdateCredential(ctx, cred); err != nil {
		return nil, err
	}
	return cred, nil
}

func (s *Service) DeleteCredential(ctx context.Context, id string) error {
	return s.store.DeleteCredential(ctx, id)
}

// ResolveSecretValue 仅在向工作站下发时解析凭证明文（禁止记入普通日志）。
func (s *Service) ResolveSecretValue(ctx context.Context, credID string) (string, error) {
	if credID == "" {
		return "", nil
	}
	cred, err := s.store.GetCredential(ctx, credID)
	if err != nil {
		return "", err
	}
	if cred.SecretRef == "" || s.secretMgr == nil {
		return "", nil
	}
	return s.secretMgr.Access(ctx, cred.SecretRef, "SYSTEM", "scheduler", "", "mcp_dispatch")
}

// ---------- Employee MCP Binding 业务逻辑 ----------

type BindEmployeeInput struct {
	MCPServerID  string         `json:"mcp_server_id"`
	CredentialID string         `json:"credential_id"`
	Enabled      *bool          `json:"enabled"`
	AllowedTools []string       `json:"allowed_tools"`
	DeniedTools  []string       `json:"denied_tools"`
	Config       map[string]any `json:"config"`
}

func (s *Service) ListBindingsByEmployee(ctx context.Context, employeeID string) ([]*EmployeeMCPBinding, error) {
	return s.store.ListBindingsByEmployee(ctx, employeeID)
}

func (s *Service) BindEmployee(ctx context.Context, employeeID string, in BindEmployeeInput) (*EmployeeMCPBinding, error) {
	if employeeID == "" || in.MCPServerID == "" {
		return nil, errors.New("缺少员工 ID 或 MCP 服务 ID")
	}
	// 验证 MCP Server 存在
	if _, err := s.store.GetServer(ctx, in.MCPServerID); err != nil {
		return nil, err
	}
	// 验证 Credential 存在（若提供）
	if in.CredentialID != "" {
		if _, err := s.store.GetCredential(ctx, in.CredentialID); err != nil {
			return nil, err
		}
	}
	// 检查是否已存在绑定
	existing, _ := s.store.GetBindingByEmployeeAndServer(ctx, employeeID, in.MCPServerID)
	if existing != nil {
		return nil, ErrBindingConflict
	}

	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}

	b := &EmployeeMCPBinding{
		ID:           idgen.New("bnd"),
		EmployeeID:   employeeID,
		MCPServerID:  in.MCPServerID,
		CredentialID: in.CredentialID,
		Enabled:      enabled,
		AllowedTools: in.AllowedTools,
		DeniedTools:  in.DeniedTools,
		Config:       in.Config,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
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

	if err := s.store.CreateBinding(ctx, b); err != nil {
		return nil, err
	}
	return s.store.GetBinding(ctx, b.ID)
}

func (s *Service) UpdateBinding(ctx context.Context, bindingID string, in BindEmployeeInput) (*EmployeeMCPBinding, error) {
	b, err := s.store.GetBinding(ctx, bindingID)
	if err != nil {
		return nil, err
	}
	if in.CredentialID != "" {
		if _, err := s.store.GetCredential(ctx, in.CredentialID); err != nil {
			return nil, err
		}
		b.CredentialID = in.CredentialID
	}
	if in.Enabled != nil {
		b.Enabled = *in.Enabled
	}
	if in.AllowedTools != nil {
		b.AllowedTools = in.AllowedTools
	}
	if in.DeniedTools != nil {
		b.DeniedTools = in.DeniedTools
	}
	if in.Config != nil {
		b.Config = in.Config
	}
	b.UpdatedAt = time.Now()

	if err := s.store.UpdateBinding(ctx, b); err != nil {
		return nil, err
	}
	return s.store.GetBinding(ctx, bindingID)
}

func (s *Service) UnbindEmployee(ctx context.Context, bindingID string) error {
	return s.store.DeleteBinding(ctx, bindingID)
}

// ---------- 内部辅助函数 ----------

func maskSecret(val string) string {
	val = strings.TrimSpace(val)
	if len(val) <= 8 {
		return "***"
	}
	prefixLen := 4
	if strings.HasPrefix(val, "ghp_") || strings.HasPrefix(val, "xoxb-") {
		prefixLen = 5
	}
	return val[:prefixLen] + "****" + val[len(val)-4:]
}

func randomHex(bytesLen int) string {
	b := make([]byte, bytesLen)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
