package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func TestExternalMCPSelfServiceAndAdminPolicy(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Admin"); err != nil {
		t.Fatal(err)
	}
	auditor := audit.NewMemory()
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	tokenStore := mcpauth.NewMemoryStore()
	tokens := mcpauth.NewService(tokenStore)
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	h := api.NewRouter(api.Deps{Auth: authSvc, Audit: auditor, MCPAuth: tokens,
		MCP: mcp.NewService(mcp.NewMemoryStore(), nil), WorkflowMCP: wf})
	adminSession := login(t, h)
	hash, _ := auth.HashPassword("test-password")
	// No management permissions at all: self-service is account-based.
	for _, id := range []string{"alice", "bob"} {
		if err := users.Create(ctx, &auth.User{ID: id, Username: id, PasswordHash: hash, Status: auth.StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	alice, err := authSvc.Login(ctx, "alice", "test-password", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := authSvc.Login(ctx, "bob", "test-password", "")
	if err != nil {
		t.Fatal(err)
	}
	mcpHandler := (&mcpserver.Server{WF: wf, Auth: authSvc, MCPAuth: tokens}).Handler()
	expect := func(method, path, session string, body any, want int) map[string]any {
		t.Helper()
		status, result := doJSON(t, h, method, path, session, body)
		if status != want {
			t.Fatalf("%s %s: got %d want %d", method, path, status, want)
		}
		return result
	}
	create := func(session, scope string) (string, string) {
		t.Helper()
		result := expect("POST", "/api/me/mcp-tokens", session, map[string]string{"scope": scope, "label": "My client"}, 201)
		var cfg mcp.ClientConfig
		if err := json.Unmarshal([]byte(result["content"].(string)), &cfg); err != nil {
			t.Fatal(err)
		}
		if len(cfg.MCPServers) != 1 || cfg.MCPServers["workflow-mcp"] == nil {
			t.Fatal("self-service exported third-party registry")
		}
		token := result["token"].(map[string]any)
		return strings.TrimPrefix(cfg.MCPServers["workflow-mcp"].Headers["Authorization"], "Bearer "), token["id"].(string)
	}
	call := func(secret, tool string, args map[string]any, wantStatus int, wantError bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args}})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		rr := httptest.NewRecorder()
		mcpHandler.ServeHTTP(rr, req)
		var resp struct {
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		if rr.Code != wantStatus || (wantStatus == 200 && resp.Result.IsError != wantError) {
			t.Fatalf("%s: status=%d isError=%v", tool, rr.Code, resp.Result.IsError)
		}
	}
	if expect("GET", "/api/me/mcp-policy", alice.Token, nil, 200)["can_write"] != false {
		t.Fatal("default policy is not read-only")
	}
	readSecret, readID := create(alice.Token, "")
	readToken, _ := tokens.Get(ctx, readID)
	if readToken.Scope != mcpauth.ScopeRead || readToken.SubjectID != "alice" {
		t.Fatal("default token not read-only and user-bound")
	}
	call(readSecret, "list_workflows", nil, 200, false)
	args := map[string]any{"path": "shared.md", "content": "# Shared\ncontent"}
	call(readSecret, "upsert_knowledge", args, 200, true)
	expect("POST", "/api/me/mcp-tokens", alice.Token, map[string]string{"scope": "WRITE"}, 403)
	expect("POST", "/api/me/mcp-tokens", alice.Token, map[string]string{"scope": "ADMIN"}, 400)
	expect("POST", "/api/me/mcp-tokens", alice.Token, map[string]string{"subject_id": "bob"}, 400)
	expect("PUT", "/api/users/alice/mcp-policy", alice.Token, map[string]bool{"can_write": true}, 403)
	expect("PUT", "/api/users/bob/mcp-policy", alice.Token, map[string]bool{"can_write": true}, 403)
	expect("GET", "/api/users/bob/mcp-policy", alice.Token, nil, 403)
	expect("POST", "/api/mcp-servers/export", alice.Token, map[string]string{}, 403)
	expect("PUT", "/api/users/alice/mcp-policy", adminSession, map[string]bool{"can_write": true}, 200)
	expect("PUT", "/api/users/missing/mcp-policy", adminSession, map[string]bool{"can_write": true}, 404)
	expect("PUT", "/api/users/alice/mcp-policy", adminSession, map[string]string{}, 400)
	writeSecret, writeID := create(alice.Token, "WRITE")
	// The selected user can write, while another user and old READ tokens cannot.
	expect("POST", "/api/me/mcp-tokens", bob.Token, map[string]string{"scope": "WRITE"}, 403)
	call(writeSecret, "upsert_knowledge", args, 200, false)
	call(readSecret, "upsert_knowledge", args, 200, true)
	call(writeSecret, "delete_knowledge", map[string]any{"id": "shared.md"}, 200, true)
	expect("GET", "/api/me/mcp-policy", writeSecret, nil, 401)
	expect("DELETE", "/api/me/mcp-tokens/"+writeID, bob.Token, nil, 404)
	bobList := expect("GET", "/api/me/mcp-tokens", bob.Token, nil, 200)
	if items, ok := bobList["items"].([]any); ok && len(items) != 0 {
		t.Fatal("another user's tokens leaked")
	}
	list := expect("GET", "/api/me/mcp-tokens", alice.Token, nil, 200)
	encoded, _ := json.Marshal(list)
	if strings.Contains(string(encoded), readSecret) || strings.Contains(string(encoded), writeSecret) {
		t.Fatal("list leaked credentials")
	}
	expect("PUT", "/api/users/alice/mcp-policy", adminSession, map[string]bool{"can_write": false}, 200)
	call(writeSecret, "upsert_knowledge", args, 200, true)
	call(writeSecret, "list_workflows", nil, 200, false)
	expect("POST", "/api/me/mcp-tokens", alice.Token, map[string]string{"scope": "WRITE"}, 403)
	expect("DELETE", "/api/me/mcp-tokens/"+readID, alice.Token, nil, 204)
	call(readSecret, "list_workflows", nil, 401, false)
	// Previously issued external ADMIN scope cannot bypass the new policy.
	legacy, _ := tokens.Get(ctx, writeID)
	legacy.Scope = mcpauth.ScopeAdmin
	if err := tokenStore.Update(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	call(writeSecret, "list_workflows", nil, 401, false)
	disabledSecret, _ := create(alice.Token, "READ")
	u, _ := users.FindByID(ctx, "alice")
	u.Status = auth.StatusDisabled
	_ = users.Update(ctx, u)
	call(disabledSecret, "list_workflows", nil, 401, false)
	expect("POST", "/api/me/mcp-tokens", alice.Token, map[string]string{}, 403)
	// Self-service endpoints retain the platform's administrator MFA gate.
	authSvc.EnforceAdminMFA = true
	expect("POST", "/api/me/mcp-tokens", adminSession, map[string]string{}, 403)
	expect("PUT", "/api/users/bob/mcp-policy", adminSession, map[string]bool{"can_write": true}, 403)
}
