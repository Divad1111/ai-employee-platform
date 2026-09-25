package api_test

import (
	"fmt"
	"testing"
)

func TestMCPServersAPI(t *testing.T) {
	h, tok := setupAPI(t)

	// 1. GET /api/mcp-servers 包含内置的 workflow-mcp
	code, body := doJSON(t, h, "GET", "/api/mcp-servers", tok, nil)
	if code != 200 {
		t.Fatalf("list mcp-servers failed: %d %v", code, body)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("expected at least 1 server (workflow-mcp), got %v", body)
	}

	// 2. POST /api/mcp-servers 创建自定义 MCP
	code, createBody := doJSON(t, h, "POST", "/api/mcp-servers", tok, map[string]any{
		"name":        "jira-mcp",
		"description": "Jira Task Integration",
		"transport":   "http",
		"endpoint":    "https://jira.example.com/mcp",
	})
	if code != 201 {
		t.Fatalf("create mcp-server failed: %d %v", code, createBody)
	}
	srv, _ := createBody["server"].(map[string]any)
	serverID, _ := srv["id"].(string)
	if serverID == "" {
		t.Fatalf("missing server id in response")
	}

	// 3. GET /api/mcp-servers/:id
	code, getBody := doJSON(t, h, "GET", fmt.Sprintf("/api/mcp-servers/%s", serverID), tok, nil)
	if code != 200 || getBody["server"] == nil {
		t.Fatalf("get mcp-server failed: %d %v", code, getBody)
	}

	// 4. PUT /api/mcp-servers/:id
	code, _ = doJSON(t, h, "PUT", fmt.Sprintf("/api/mcp-servers/%s", serverID), tok, map[string]any{
		"description": "Updated Jira Description",
	})
	if code != 200 {
		t.Fatalf("update mcp-server failed: %d", code)
	}

	// 5. DELETE /api/mcp-servers/:id
	code, _ = doJSON(t, h, "DELETE", fmt.Sprintf("/api/mcp-servers/%s", serverID), tok, nil)
	if code != 200 {
		t.Fatalf("delete mcp-server failed: %d", code)
	}

	// 6. 内置 workflow-mcp 禁止删除
	code, _ = doJSON(t, h, "DELETE", "/api/mcp-servers/mcp-workflow", tok, nil)
	if code != 400 {
		t.Fatalf("expected 400 when deleting builtin mcp-workflow, got %d", code)
	}
}

func TestCredentialsAndBindingsAPI(t *testing.T) {
	h, tok := setupAPI(t)

	// 1. POST /api/credentials 创建凭证
	code, credBody := doJSON(t, h, "POST", "/api/credentials", tok, map[string]any{
		"credential_name": "张三 GitHub PAT",
		"owner_type":      "USER",
		"provider":        "github",
		"auth_type":       "pat",
		"secret_value":    "ghp_secret_test_token_12345",
	})
	if code != 201 {
		t.Fatalf("create credential failed: %d %v", code, credBody)
	}
	cred, _ := credBody["credential"].(map[string]any)
	credID, _ := cred["id"].(string)
	if credID == "" {
		t.Fatalf("missing credential id")
	}

	// 2. GET /api/credentials
	code, listBody := doJSON(t, h, "GET", "/api/credentials", tok, nil)
	if code != 200 {
		t.Fatalf("list credentials failed: %d", code)
	}
	items, _ := listBody["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 credential, got %d", len(items))
	}

	// 3. POST /api/employees/:id/mcp-bindings 绑定员工与 MCP
	empID := "EMP-TEST-001"
	code, bndBody := doJSON(t, h, "POST", fmt.Sprintf("/api/employees/%s/mcp-bindings", empID), tok, map[string]any{
		"mcp_server_id": "mcp-workflow",
		"credential_id": credID,
		"allowed_tools": []string{"search_skills", "list_workflows"},
	})
	if code != 201 {
		t.Fatalf("create binding failed: %d %v", code, bndBody)
	}
	bnd, _ := bndBody["binding"].(map[string]any)
	bindingID, _ := bnd["id"].(string)

	// 4. GET /api/employees/:id/mcp-bindings
	code, empBndBody := doJSON(t, h, "GET", fmt.Sprintf("/api/employees/%s/mcp-bindings", empID), tok, nil)
	if code != 200 {
		t.Fatalf("list employee bindings failed: %d", code)
	}
	bndItems, _ := empBndBody["items"].([]any)
	if len(bndItems) != 1 {
		t.Fatalf("expected 1 binding, got %d", len(bndItems))
	}

	// 5. DELETE /api/employees/:id/mcp-bindings/:bindingId
	code, _ = doJSON(t, h, "DELETE", fmt.Sprintf("/api/employees/%s/mcp-bindings/%s", empID, bindingID), tok, nil)
	if code != 200 {
		t.Fatalf("delete binding failed: %d", code)
	}

	// 6. DELETE /api/credentials/:id
	code, _ = doJSON(t, h, "DELETE", fmt.Sprintf("/api/credentials/%s", credID), tok, nil)
	if code != 200 {
		t.Fatalf("delete credential failed: %d", code)
	}
}
