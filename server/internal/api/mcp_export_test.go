package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/config"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func TestMCPExportEndToEnd(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Admin"); err != nil {
		t.Fatal(err)
	}
	auditor := audit.NewMemory()
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	tokens := mcpauth.NewService(mcpauth.NewMemoryStore())
	registry := mcp.NewService(mcp.NewMemoryStore(), nil)
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	if _, _, err := wf.UpsertWorkflowYAML(ctx, "id: external-test\nname: External\nsteps: []\n", "patch"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{PublicServerURL: "https://platform.example", MCPPublicURL: "https://mcp.example/mcp"}
	h := api.NewRouter(api.Deps{Auth: authSvc, Audit: auditor, MCP: registry, MCPAuth: tokens, WorkflowMCP: wf, Config: cfg})
	adminToken := login(t, h)
	admin, _ := authSvc.Authenticate(ctx, adminToken)
	mcpHandler := (&mcpserver.Server{WF: wf, Auth: authSvc, MCPAuth: tokens}).Handler()
	export := func(scope string) (string, string, string) {
		t.Helper()
		req := httptest.NewRequest("POST", "/api/mcp-servers/export",
			strings.NewReader(`{"format":"json","scope":"`+scope+`"}`))
		req.Header.Set("Authorization", "Bearer "+adminToken)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 200 || rr.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("export status %d", rr.Code)
		}
		var result struct {
			Content string        `json:"content"`
			Token   mcpauth.Token `json:"token"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		var client mcp.ClientConfig
		if err := json.Unmarshal([]byte(result.Content), &client); err != nil {
			t.Fatal(err)
		}
		if result.Token.ExpiresAt == nil || time.Until(*result.Token.ExpiresAt) > 90*24*time.Hour ||
			time.Until(*result.Token.ExpiresAt) < 89*24*time.Hour {
			t.Fatal("expected 90-day authorization")
		}
		entry := client.MCPServers["workflow-mcp"]
		return strings.TrimPrefix(entry.Headers["Authorization"], "Bearer "), result.Token.ID, entry.URL
	}
	call := func(secret, tool string, arguments map[string]any) (int, bool) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": arguments}})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret)
		rr := httptest.NewRecorder()
		mcpHandler.ServeHTTP(rr, req)
		var response struct {
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		_ = json.Unmarshal(rr.Body.Bytes(), &response)
		return rr.Code, response.Result.IsError
	}
	secret, tokenID, endpoint := export(mcpauth.ScopeRead)
	if endpoint != "https://mcp.example/mcp" || secret == adminToken {
		t.Fatal("export did not use a dedicated credential and public URL")
	}
	// Production administrators' legacy USER tokens remain disallowed; explicitly
	// delegated external tokens can be used without sharing a web/MFA session.
	authSvc.EnforceAdminMFA = true
	if status, isError := call(secret, "list_workflows", nil); status != 200 || isError {
		t.Fatal("external read failed under MFA policy")
	}
	if status, isError := call(secret, "delete_workflow", map[string]any{"id": "external-test"}); status != 200 || !isError {
		t.Fatal("read-only token allowed a write")
	}
	legacy, _ := tokens.Issue(ctx, mcpauth.SubjectUser, admin.UserID, mcpauth.ScopeRead, "legacy", admin.UserID, time.Hour)
	if status, _ := call(legacy.Secret, "list_workflows", nil); status != 401 {
		t.Fatal("legacy USER token bypassed MFA")
	}
	if code, _ := doJSON(t, h, "POST", "/api/mcp-servers/export", adminToken, map[string]string{}); code != 403 {
		t.Fatal("new delegation bypassed management MFA")
	}
	authSvc.EnforceAdminMFA = false
	if code, _ := doJSON(t, h, "GET", "/api/mcp-servers", secret, nil); code != 401 {
		t.Fatal("MCP credential was accepted as a management session")
	}
	if code, _ := doJSON(t, h, "POST", "/api/mcp-servers/export", adminToken, map[string]string{"scope": "WRITE"}); code != 403 {
		t.Fatal("administrator implicitly received external write permission")
	}
	if code, _ := doJSON(t, h, "PUT", "/api/users/"+admin.UserID+"/mcp-policy", adminToken, map[string]bool{"can_write": true}); code != 200 {
		t.Fatal("explicit policy grant failed")
	}
	writeSecret, _, _ := export(mcpauth.ScopeWrite)
	if status, isError := call(writeSecret, "upsert_knowledge", map[string]any{"path": "test.md", "content": "# Test\nhello"}); status != 200 || isError {
		t.Fatal("explicit write delegation failed")
	}
	if code, _ := doJSON(t, h, "DELETE", "/api/mcp-tokens/"+tokenID, adminToken, nil); code != 204 {
		t.Fatal("failed to revoke exported token")
	}
	if status, _ := call(secret, "list_workflows", nil); status != 401 {
		t.Fatal("revoked export still works")
	}
	listCode, list := doJSON(t, h, "GET", "/api/mcp-servers/export-tokens", adminToken, nil)
	listJSON, _ := json.Marshal(list)
	if listCode != 200 || strings.Contains(string(listJSON), secret) || strings.Contains(string(listJSON), writeSecret) {
		t.Fatal("export token list leaked secrets")
	}
	// External policy is separate from management RBAC. Removing global roles
	// prevents registry exports; revoking the explicit grant disables writes.
	if err := users.CreateRole(ctx, "OWN_MCP", "", []auth.PermissionGrant{
		{Code: "workflow.read", Scope: "OWN"}, {Code: "workflow.grant", Scope: "OWN"},
	}); err != nil {
		t.Fatal(err)
	}
	_ = users.SetRoles(ctx, admin.UserID, []string{"OWN_MCP"})
	if status, isError := call(writeSecret, "list_workflows", nil); status != 200 || isError {
		t.Fatal("self-service read access incorrectly depends on global roles")
	}
	_, _ = tokens.SetExternalPolicy(ctx, admin.UserID, false, "policy-admin")
	if status, isError := call(writeSecret, "upsert_knowledge", map[string]any{"path": "test.md", "content": "blocked"}); status != 200 || !isError {
		t.Fatal("write survived removal of explicit grant")
	}
	if code, _ := doJSON(t, h, "POST", "/api/mcp-servers/export", adminToken, map[string]string{}); code != 403 {
		t.Fatal("OWN user exported global MCP configuration")
	}
}

func TestMCPExportValidationDoesNotIssueToken(t *testing.T) {
	h, token := setupAPI(t)
	if code, _ := doJSON(t, h, "POST", "/api/mcp-servers", token, map[string]any{
		"name": "bad-endpoint", "transport": "http", "endpoint": "/relative",
	}); code != 201 {
		t.Fatal("setup failed")
	}
	if code, _ := doJSON(t, h, "POST", "/api/mcp-servers/export", token, map[string]string{"format": "json"}); code != 400 {
		t.Fatal("invalid service should fail the whole export")
	}
	_, list := doJSON(t, h, "GET", "/api/mcp-servers/export-tokens", token, nil)
	if items, ok := list["items"].([]any); ok && len(items) > 0 {
		t.Fatal("failed export created unused credentials")
	}
}

func TestMCPExportBrowserOrigin(t *testing.T) {
	h, token := setupAPI(t)
	req := httptest.NewRequest("POST", "http://192.0.2.10:8088/api/mcp-servers/export", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-AIE-Browser", "1")
	req.Header.Set("Origin", "http://192.0.2.10:8088")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var result struct {
		Content string `json:"content"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &result)
	if rr.Code != 200 || !strings.Contains(result.Content, "http://192.0.2.10:8088/mcp") {
		t.Fatal("browser-facing port was lost from exported URL")
	}
	req.Header.Set("Origin", "http://other.example")
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatal("cross-origin export was accepted")
	}
}
