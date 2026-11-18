package scheduler

import (
	"context"
	"errors"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/employee"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/secret"
)

func TestBuiltinDispatchUsesSeparateServiceTokens(t *testing.T) {
	ctx := context.Background()
	registry := mcp.NewService(mcp.NewMemoryStore(), nil)
	employees := employee.NewService(employee.NewMemoryStore(), audit.NewMemory(), nil)
	e, err := employees.Create(ctx, employee.CreateInput{Name: "collector", OwnerUserID: "owner"}, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	tokens := mcpauth.NewService(mcpauth.NewMemoryStore())
	s := &Service{MCP: registry, Employees: employees, MCPAuth: tokens, MCPPublicURL: "https://platform.example/mcp"}
	for _, id := range []string{"mcp-workflow", "mcp-collector"} {
		if _, err := registry.BindEmployee(ctx, e.ID, mcp.BindEmployeeInput{MCPServerID: id}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.buildStartJobPayload(ctx, e.ID, "hello", "workspace", "path", "")
	if err != nil || len(first.McpServers) != 2 {
		t.Fatalf("dispatch: %v %v", first, err)
	}
	second, err := s.buildStartJobPayload(ctx, e.ID, "hello", "workspace", "path", "")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for i, spec := range first.McpServers {
		raw := strings.TrimPrefix(spec.Headers["Authorization"], "Bearer ")
		token, err := tokens.Validate(ctx, raw)
		if err != nil || token.Service != spec.Name || token.SubjectID != e.ID || token.Scope != mcpauth.ScopeRead {
			t.Fatalf("wrong service token: %v %v", token, err)
		}
		if second.McpServers[i].Headers["Authorization"] != spec.Headers["Authorization"] {
			t.Fatal("session token changed unnecessarily")
		}
		if spec.Name == mcpauth.Collector && spec.Url != "https://platform.example/mcp/collector" {
			t.Fatal(spec.Url)
		}
		seen[spec.Name] = raw
	}
	if seen[mcpauth.Workflow] == seen[mcpauth.Collector] {
		t.Fatal("service audiences share token")
	}
}

func TestExternalMCPAuthenticationDispatch(t *testing.T) {
	ctx := context.Background()
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	mcpSvc := mcp.NewService(mcp.NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), nil))
	employees := employee.NewService(employee.NewMemoryStore(), audit.NewMemory(), nil)
	e, err := employees.Create(ctx, employee.CreateInput{Name: "test", OwnerUserID: "user"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{MCP: mcpSvc, Employees: employees}
	for _, tc := range []struct {
		name, transport, authType string
		metadata                  map[string]any
	}{
		{"jira", "http", mcp.AuthTypeToken, nil},
		{"unity", "stdio", mcp.AuthTypeAPIKey, map[string]any{"env_name": "UNITY_TOKEN"}},
	} {
		srv, err := mcpSvc.CreateServer(ctx, mcp.CreateServerInput{Name: tc.name, Transport: tc.transport, Endpoint: "endpoint", Config: map[string]any{"headers": map[string]any{"authorization": "old", "X-Project": "A"}}})
		if err != nil {
			t.Fatal(err)
		}
		cred, err := mcpSvc.CreateCredential(ctx, mcp.CreateCredentialInput{OwnerType: "USER", OwnerID: "user", CredentialName: tc.name, AuthType: tc.authType, SecretValue: "test-key", Metadata: tc.metadata})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mcpSvc.BindEmployee(ctx, e.ID, mcp.BindEmployeeInput{MCPServerID: srv.ID, CredentialID: cred.ID}); err != nil {
			t.Fatal(err)
		}
	}
	// 外部 MCP 配置不应依赖内置 workflow-mcp 已启用。
	start, err := s.buildStartJobPayload(ctx, e.ID, "hello", "workspace", "path", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(start.McpServers) != 2 {
		t.Fatal("external MCP configurations missing")
	}
	for _, server := range start.McpServers {
		switch server.Name {
		case "jira":
			if len(server.Headers) != 2 || server.Headers["Authorization"] != "Token test-key" || server.Headers["X-Project"] != "A" {
				t.Fatal("credential did not override the case-insensitive authentication header")
			}
		case "unity":
			if server.Env["UNITY_TOKEN"] != "test-key" {
				t.Fatal("STDIO credential missing")
			}
		}
	}
}

func TestExpiredMCPPreventsDispatch(t *testing.T) {
	ctx := context.Background()
	mcpSvc := mcp.NewService(mcp.NewMemoryStore(), nil)
	srv, err := mcpSvc.CreateServer(ctx, mcp.CreateServerInput{Name: "expired", Transport: "http", Endpoint: "http://localhost/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(-time.Minute)
	cred, err := mcpSvc.CreateCredential(ctx, mcp.CreateCredentialInput{OwnerType: "USER", OwnerID: "user", CredentialName: "expired", ExpiresAt: &expiry})
	if err != nil {
		t.Fatal(err)
	}
	employees := employee.NewService(employee.NewMemoryStore(), audit.NewMemory(), nil)
	e, err := employees.Create(ctx, employee.CreateInput{Name: "test", OwnerUserID: "user"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{MCP: mcpSvc, Employees: employees}
	if _, err := mcpSvc.BindEmployee(ctx, e.ID, mcp.BindEmployeeInput{MCPServerID: srv.ID, CredentialID: cred.ID}); err != nil {
		t.Fatal(err)
	}
	start, err := s.buildStartJobPayload(ctx, e.ID, "hello", "workspace", "path", "")
	if !errors.Is(err, mcp.ErrCredentialExpired) || start != nil {
		t.Fatalf("expected rejected dispatch, got %v", err)
	}
}
