package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/collector"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func TestCollectorSelfServiceIsolationAndServiceAudience(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Admin"); err != nil {
		t.Fatal(err)
	}
	aud := audit.NewMemory()
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), aud)
	tokens := mcpauth.NewService(mcpauth.NewMemoryStore())
	registry := mcp.NewService(mcp.NewMemoryStore(), nil)
	vault, _ := secret.NewMemoryVault()
	service := collector.NewService(collector.NewMemoryStore(), secret.NewManager(vault, secret.NewMemoryBindings(), aud))
	service.UserActive = func(ctx context.Context, id string) bool {
		u, err := users.FindByID(ctx, id)
		return err == nil && u.Status == auth.StatusActive
	}
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	handler := api.NewRouter(api.Deps{Auth: authSvc, Audit: aud, MCPAuth: tokens, MCP: registry, Collector: service, WorkflowMCP: wf})
	admin := login(t, handler)
	hash, _ := auth.HashPassword("test-password")
	for _, id := range []string{"alice", "bob"} {
		if err := users.Create(ctx, &auth.User{ID: id, Username: id, PasswordHash: hash, Status: auth.StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	alice, _ := authSvc.Login(ctx, "alice", "test-password", "")
	bob, _ := authSvc.Login(ctx, "bob", "test-password", "")
	expect := func(method, path, session string, body any, status int) map[string]any {
		t.Helper()
		code, out := doJSON(t, handler, method, path, session, body)
		if code != status {
			t.Fatalf("%s %s: %d %v", method, path, code, out)
		}
		return out
	}
	profile := map[string]any{"display_name": "Alice", "timezone": "Asia/Shanghai", "credentials": map[string]string{"jira_token": "synthetic-alice-secret"}}
	status := expect("PUT", "/api/collector/profile", alice.Token, profile, 200)
	b, _ := json.Marshal(status)
	if strings.Contains(string(b), "synthetic-alice-secret") || strings.Contains(string(b), "secret_ref") {
		t.Fatal("profile leaked secret")
	}
	target := expect("POST", "/api/collector/targets", alice.Token, map[string]any{"name": "Private Jira", "source": "jira", "config": map[string]string{"jql": "assignee = currentUser()"}}, 200)
	id := target["id"].(string)
	expect("GET", "/api/collector/records/target?owner_id=alice", bob.Token, nil, 403)
	expect("DELETE", "/api/collector/records/target/"+id, bob.Token, nil, 204)
	items := expect("GET", "/api/collector/records/target", alice.Token, nil, 200)
	if len(items["items"].([]any)) != 1 {
		t.Fatal("bob deleted alice target")
	}
	expect("PUT", "/api/users/alice/mcp-policy", admin, map[string]any{"service": mcpauth.Collector, "can_write": true}, 200)
	policy := expect("GET", "/api/me/mcp-policy", alice.Token, nil, 200)
	if policy["can_write"] != false || policy["collector_can_write"] != true {
		t.Fatal("service policies coupled")
	}
	result := expect("POST", "/api/me/mcp-tokens", alice.Token, map[string]any{"services": []any{
		map[string]string{"service": mcpauth.Workflow, "scope": "READ"},
		map[string]string{"service": mcpauth.Collector, "scope": "WRITE"},
	}}, 201)
	var cfg mcp.ClientConfig
	if err := json.Unmarshal([]byte(result["content"].(string)), &cfg); err != nil {
		t.Fatal(err)
	}
	collectorSecret := strings.TrimPrefix(cfg.MCPServers[mcpauth.Collector].Headers["Authorization"], "Bearer ")
	workflowSecret := strings.TrimPrefix(cfg.MCPServers[mcpauth.Workflow].Headers["Authorization"], "Bearer ")
	if collectorSecret == workflowSecret {
		t.Fatal("builtins shared a credential")
	}
	collectorHandler := (&mcpserver.Server{Auth: authSvc, MCPAuth: tokens, Provider: &collector.Provider{Service: service}}).Handler()
	workflowHandler := (&mcpserver.Server{Auth: authSvc, MCPAuth: tokens, WF: wf}).Handler()
	call := func(h http.Handler, token, name string, args map[string]any) (int, bool) {
		payload, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(string(payload)))
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var result struct {
			Result struct {
				IsError bool `json:"isError"`
			} `json:"result"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		return rec.Code, result.Result.IsError
	}
	if code, _ := call(collectorHandler, workflowSecret, "whoami", nil); code != 401 {
		t.Fatal("workflow credential accessed collector")
	}
	if code, _ := call(workflowHandler, collectorSecret, "list_workflows", nil); code != 401 {
		t.Fatal("collector credential accessed workflow")
	}
	if code, e := call(collectorHandler, collectorSecret, "whoami", nil); code != 200 || e {
		t.Fatal("self-service collector auth failed")
	}
	writeArgs := map[string]any{"open_id": "ou-other", "text": "preview only"}
	if code, e := call(collectorHandler, collectorSecret, "send_feishu_message", writeArgs); code != 200 || e {
		t.Fatal("explicit write grant did not allow preview")
	}
	expect("PUT", "/api/users/alice/mcp-policy", admin, map[string]any{"service": mcpauth.Collector, "can_write": false}, 200)
	if _, e := call(collectorHandler, collectorSecret, "send_feishu_message", writeArgs); !e {
		t.Fatal("write revocation not effective")
	}
	expect("GET", "/api/collector/profile", collectorSecret, nil, 401)
}
