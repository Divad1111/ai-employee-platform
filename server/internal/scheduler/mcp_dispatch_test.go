package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/secret"
)

func TestExternalMCPAuthenticationDispatch(t *testing.T) {
	ctx := context.Background()
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	mcpSvc := mcp.NewService(mcp.NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), nil))
	s := &Service{MCP: mcpSvc}
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
		cred, err := mcpSvc.CreateCredential(ctx, mcp.CreateCredentialInput{CredentialName: tc.name, AuthType: tc.authType, SecretValue: "test-key", Metadata: tc.metadata})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := mcpSvc.BindEmployee(ctx, "EMP", mcp.BindEmployeeInput{MCPServerID: srv.ID, CredentialID: cred.ID}); err != nil {
			t.Fatal(err)
		}
	}
	// 外部 MCP 配置不应依赖内置 workflow-mcp 已启用。
	start, err := s.buildStartJobPayload(ctx, "EMP", "hello", "workspace", "path", "")
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
	cred, err := mcpSvc.CreateCredential(ctx, mcp.CreateCredentialInput{CredentialName: "expired", ExpiresAt: &expiry})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mcpSvc.BindEmployee(ctx, "EMP", mcp.BindEmployeeInput{MCPServerID: srv.ID, CredentialID: cred.ID}); err != nil {
		t.Fatal(err)
	}
	s := &Service{MCP: mcpSvc}
	start, err := s.buildStartJobPayload(ctx, "EMP", "hello", "workspace", "path", "")
	if !errors.Is(err, mcp.ErrCredentialExpired) || start != nil {
		t.Fatalf("expected rejected dispatch, got %v", err)
	}
}
