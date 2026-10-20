package scheduler

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/secret"
)

// Changing the linked credential must change the next dispatch without editing
// the MCP server or employee binding. No provider-specific Basic/Bearer override.
func TestDispatchFollowsLinkedCredentialChanges(t *testing.T) {
	ctx := context.Background()
	vault, _ := secret.NewMemoryVault()
	svc := mcp.NewService(mcp.NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), nil))
	srv, err := svc.CreateServer(ctx, mcp.CreateServerInput{Name: "external", Transport: "http", Endpoint: "https://example.test/mcp"})
	if err != nil {
		t.Fatal(err)
	}
	cred, err := svc.CreateCredential(ctx, mcp.CreateCredentialInput{CredentialName: "linked", AuthType: mcp.AuthTypeBasic, SecretValue: "user:pass"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.BindEmployee(ctx, "EMP-test", mcp.BindEmployeeInput{MCPServerID: srv.ID, CredentialID: cred.ID}); err != nil {
		t.Fatal(err)
	}
	s := &Service{MCP: svc}
	for _, tc := range []struct {
		authType, secret, header, want string
		metadata                       map[string]any
	}{
		{mcp.AuthTypeBasic, "user:pass", "Authorization", "Basic dXNlcjpwYXNz", nil},
		{mcp.AuthTypeToken, "new-token", "Authorization", "Token new-token", nil},
		{mcp.AuthTypeBearer, "bearer-token", "Authorization", "Bearer bearer-token", nil},
		{mcp.AuthTypeAPIKey, "api-key", "X-Api-Key", "api-key", map[string]any{"header_name": "X-API-Key"}},
	} {
		if _, err := svc.UpdateCredential(ctx, cred.ID, mcp.CreateCredentialInput{AuthType: tc.authType, SecretValue: tc.secret, Metadata: tc.metadata}); err != nil {
			t.Fatal(err)
		}
		payload, err := s.buildStartJobPayload(ctx, "EMP-test", "hello", "ws", "path", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(payload.McpServers) != 1 || len(payload.McpServers[0].Headers) != 1 || payload.McpServers[0].Headers[tc.header] != tc.want {
			t.Fatalf("dispatch ignored linked %s credential", tc.authType)
		}
	}
}
