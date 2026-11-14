package mcpserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func TestMCPInitializedNotificationHandshake(t *testing.T) {
	tokens := mcpauth.NewService(mcpauth.NewMemoryStore())
	token, err := tokens.Issue(context.Background(), mcpauth.SubjectEmployee, "EMP-HANDSHAKE", mcpauth.ScopeRead, "test", "test", 0)
	if err != nil {
		t.Fatal(err)
	}
	h := (&mcpserver.Server{WF: workflowmcp.NewService(workflowmcp.NewMemoryStore()), MCPAuth: tokens}).Handler()

	// rmcp starts request IDs at zero; the initialize response must retain it.
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`))
	req.Header.Set("Authorization", "Bearer "+token.Secret)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var initialized struct {
		ID     *int           `json:"id"`
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &initialized); err != nil || rr.Code != http.StatusOK || initialized.ID == nil || *initialized.ID != 0 || initialized.Result["protocolVersion"] == nil {
		t.Fatalf("invalid initialize response: status=%d body=%s error=%v", rr.Code, rr.Body.String(), err)
	}

	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	req.Header.Set("Authorization", "Bearer "+token.Secret)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted || rr.Body.Len() != 0 {
		t.Fatalf("notification must return empty 202: status=%d body=%s", rr.Code, rr.Body.String())
	}
	tools, rpcErr := callRPC(t, h, token.Secret, "tools/list", nil)
	if rpcErr != nil || len(tools["tools"].([]any)) == 0 {
		t.Fatalf("tools unavailable after handshake: result=%v error=%v", tools, rpcErr)
	}

	// Notifications still require the employee's credential.
	req = httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated notification accepted: %d", rr.Code)
	}
}
