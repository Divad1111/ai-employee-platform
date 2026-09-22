package mcpserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func TestMCPAuthMatrix(t *testing.T) {
	ctx := context.Background()
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	_, _, _ = wf.UpsertWorkflowYAML(ctx, `id: wf-a
name: A
version: 1.0.0
skills: []
knowledge: []
steps:
  - id: s1
    description: x
`, "patch")
	_, _, _ = wf.UpsertWorkflowYAML(ctx, `id: wf-b
name: B
version: 1.0.0
skills: []
knowledge: []
steps:
  - id: s1
    description: x
`, "patch")
	_ = wf.GrantWorkflow(ctx, "EMP-1", "wf-a", "admin")

	tokSvc := mcpauth.NewService(mcpauth.NewMemoryStore())
	empTok, err := tokSvc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-1", mcpauth.ScopeRead, "t", "admin", 0)
	if err != nil {
		t.Fatal(err)
	}

	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin123", "Admin")
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	sess, err := authSvc.Login(ctx, "admin", "admin123", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}

	srv := &mcpserver.Server{WF: wf, MCPAuth: tokSvc, Auth: authSvc}
	h := srv.Handler()

	// Employee READ: upsert 被拒
	out := callTool(t, h, empTok.Secret, "upsert_workflow", map[string]any{
		"content": "id: wf-x\nname: X\nversion: 1.0.0\nsteps: []\n",
	})
	if isErr, _ := out["isError"].(bool); !isErr {
		t.Fatalf("expected write denied: %v", out)
	}

	// Admin session: 可写
	out = callTool(t, h, sess.Token, "upsert_workflow", map[string]any{
		"content": "id: wf-x\nname: X\nversion: 1.0.0\nsteps:\n  - id: a\n    description: a\n",
	})
	if isErr, _ := out["isError"].(bool); isErr {
		t.Fatalf("admin write failed: %v", out)
	}
}

func callTool(t *testing.T, h http.Handler, token, name string, args map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.Error != nil {
		t.Fatalf("rpc error %v", resp.Error)
	}
	return resp.Result
}

func callRPC(t *testing.T, h http.Handler, token, method string, params any) (map[string]any, any) {
	t.Helper()
	var rawParams json.RawMessage
	if params != nil {
		rawParams, _ = json.Marshal(params)
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": method,
		"params": rawParams,
	})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("status %d %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Result map[string]any `json:"result"`
		Error  any            `json:"error"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	return resp.Result, resp.Error
}

func TestMCPProtocolRPC(t *testing.T) {
	ctx := context.Background()
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	tokSvc := mcpauth.NewService(mcpauth.NewMemoryStore())
	empTok, err := tokSvc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-PROTO", mcpauth.ScopeRead, "t", "admin", 0)
	if err != nil {
		t.Fatal(err)
	}

	srv := &mcpserver.Server{WF: wf, MCPAuth: tokSvc}
	h := srv.Handler()

	// 1. GET /mcp
	getReq := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	if getRR.Code != 200 || !strings.Contains(getRR.Body.String(), "workflow-mcp") {
		t.Fatalf("GET /mcp failed: %d %s", getRR.Code, getRR.Body.String())
	}

	// 2. initialize
	initRes, errVal := callRPC(t, h, empTok.Secret, "initialize", nil)
	if errVal != nil || initRes["protocolVersion"] != "2024-11-05" {
		t.Fatalf("initialize failed: err=%v, res=%+v", errVal, initRes)
	}

	// 3. ping
	pingRes, errVal := callRPC(t, h, empTok.Secret, "ping", nil)
	if errVal != nil || pingRes == nil {
		t.Fatalf("ping failed: err=%v", errVal)
	}

	// 4. tools/list
	toolsRes, errVal := callRPC(t, h, empTok.Secret, "tools/list", nil)
	if errVal != nil {
		t.Fatalf("tools/list failed: %v", errVal)
	}
	toolsList, ok := toolsRes["tools"].([]any)
	if !ok || len(toolsList) == 0 {
		t.Fatalf("expected non-empty tools list: %+v", toolsRes)
	}
}

type fakeSyncer struct {
	synced map[string][]string
}

func (f *fakeSyncer) SyncSkills(_ context.Context, workstationID, employeeID string, skillIDs []string) error {
	if f.synced == nil {
		f.synced = map[string][]string{}
	}
	f.synced[workstationID] = append(f.synced[workstationID], skillIDs...)
	return nil
}

func TestMCPReadToolsAndScopeFilter(t *testing.T) {
	ctx := context.Background()
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	// 准备两个工作流、技能、知识
	_, _, _ = wf.UpsertSkillMD(ctx, `---
name: sk-ok
id: skill.ok
version: 1.0.0
---
body ok
`, nil, "patch")

	_, _, _ = wf.UpsertSkillMD(ctx, `---
name: sk-secret
id: skill.secret
version: 1.0.0
---
body secret
`, nil, "patch")

	_, _, _ = wf.UpsertKnowledgeMD(ctx, "public/guide.md", `---
title: Guide
namespace: public
---
# Guide
Public doc
`, "patch")

	_, _, _ = wf.UpsertKnowledgeMD(ctx, "secret/doc.md", `---
title: Secret
namespace: secret
---
# Secret
Private doc
`, "patch")

	_, _, _ = wf.UpsertWorkflowYAML(ctx, `id: wf-allowed
name: Allowed
version: 1.0.0
skills:
  - skill.ok
knowledge:
  - public
steps:
  - id: s1
    description: d
`, "patch")

	_, _, _ = wf.UpsertWorkflowYAML(ctx, `id: wf-forbidden
name: Forbidden
version: 1.0.0
skills:
  - skill.secret
knowledge:
  - secret
steps:
  - id: s1
    description: d
`, "patch")

	// 授权 EMP-SCOPED 仅访问 wf-allowed
	_ = wf.GrantWorkflow(ctx, "EMP-SCOPED", "wf-allowed", "admin")

	tokSvc := mcpauth.NewService(mcpauth.NewMemoryStore())
	empTok, err := tokSvc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-SCOPED", mcpauth.ScopeRead, "t", "admin", 0)
	if err != nil {
		t.Fatal(err)
	}

	syncer := &fakeSyncer{}
	srv := &mcpserver.Server{WF: wf, MCPAuth: tokSvc, Syncer: syncer}
	h := srv.Handler()

	// 1. list_workflows: 仅包含 wf-allowed
	out := callTool(t, h, empTok.Secret, "list_workflows", map[string]any{})
	var contentArr []map[string]any
	b, _ := json.Marshal(out["content"])
	_ = json.Unmarshal(b, &contentArr)
	if len(contentArr) == 0 || !strings.Contains(contentArr[0]["text"].(string), "wf-allowed") {
		t.Fatalf("expected wf-allowed in list: %+v", out)
	}
	if strings.Contains(contentArr[0]["text"].(string), "wf-forbidden") {
		t.Fatalf("wf-forbidden should be filtered: %+v", out)
	}

	// 2. get_workflow 未授权拦截
	outDenied := callTool(t, h, empTok.Secret, "get_workflow", map[string]any{"id": "wf-forbidden"})
	if isErr, _ := outDenied["isError"].(bool); !isErr {
		t.Fatalf("expected unauthorized get_workflow error: %+v", outDenied)
	}

	// 3. get_workflow 已授权成功
	outAllowed := callTool(t, h, empTok.Secret, "get_workflow", map[string]any{"id": "wf-allowed"})
	if isErr, _ := outAllowed["isError"].(bool); isErr {
		t.Fatalf("expected successful get_workflow: %+v", outAllowed)
	}

	// 4. get_skill 未授权拦截
	outSkillDenied := callTool(t, h, empTok.Secret, "get_skill", map[string]any{"id": "skill.secret"})
	if isErr, _ := outSkillDenied["isError"].(bool); !isErr {
		t.Fatalf("expected unauthorized get_skill error: %+v", outSkillDenied)
	}

	// 5. search_knowledge & search_cases
	outK := callTool(t, h, empTok.Secret, "search_knowledge", map[string]any{"query": "Public", "limit": 5})
	if isErr, _ := outK["isError"].(bool); isErr {
		t.Fatalf("search_knowledge failed: %+v", outK)
	}

	outCases := callTool(t, h, empTok.Secret, "search_cases", map[string]any{"query": "anything", "limit": 5})
	if isErr, _ := outCases["isError"].(bool); isErr {
		t.Fatalf("search_cases failed: %+v", outCases)
	}

	// 6. sync_skill 拒绝只读员工调用
	outSyncDenied := callTool(t, h, empTok.Secret, "sync_skill", map[string]any{
		"id": "skill.ok", "workstation_id": "WS-1", "employee_id": "EMP-SCOPED",
	})
	if isErr, _ := outSyncDenied["isError"].(bool); !isErr {
		t.Fatalf("expected write denied for sync_skill: %+v", outSyncDenied)
	}
}
