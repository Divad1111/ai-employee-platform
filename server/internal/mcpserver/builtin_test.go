package mcpserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/mcpserver"
)

type personalProvider struct{}

func (personalProvider) Name() string { return mcpauth.Collector }
func (personalProvider) Tools() []map[string]any {
	return []map[string]any{{"name": "whoami"}, {"name": "denied"}}
}
func (personalProvider) Call(_ context.Context, c *mcpserver.Caller, _ string, _ map[string]any) (any, error) {
	return map[string]string{"owner": c.UserID}, nil
}

func TestEmployeeBuiltinRechecksBindingOwnerAndServiceState(t *testing.T) {
	tokens := mcpauth.NewService(mcpauth.NewMemoryStore())
	issued, err := tokens.IssueFor(context.Background(), mcpauth.Collector, mcpauth.SubjectEmployee, "employee", mcpauth.ScopeRead, "session", "scheduler", 0)
	if err != nil {
		t.Fatal(err)
	}
	enabled, bound, owner := true, true, "alice"
	srv := &mcpserver.Server{MCPAuth: tokens, Provider: personalProvider{},
		Enabled: func(context.Context, string) bool { return enabled },
		ResolveEmployee: func(context.Context, string, string) (string, []string, []string, error) {
			if !bound {
				return "", nil, nil, errors.New("unbound")
			}
			return owner, []string{"whoami"}, nil, nil
		}}
	request := func(method, params string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/mcp/collector", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		req.Header.Set("Authorization", "Bearer "+issued.Secret)
		out := httptest.NewRecorder()
		srv.Handler().ServeHTTP(out, req)
		return out
	}
	response := request("tools/list", `{}`)
	var listed struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &listed)
	if len(listed.Result.Tools) != 1 || listed.Result.Tools[0]["name"] != "whoami" {
		t.Fatal("tool whitelist not applied")
	}
	if !strings.Contains(request("tools/call", `{"name":"whoami"}`).Body.String(), "alice") {
		t.Fatal("owner not resolved")
	}
	owner = "bob"
	if !strings.Contains(request("tools/call", `{"name":"whoami"}`).Body.String(), "bob") {
		t.Fatal("stale owner authorization")
	}
	if !strings.Contains(request("tools/call", `{"name":"denied"}`).Body.String(), `"isError":true`) {
		t.Fatal("direct call bypassed tool whitelist")
	}
	bound = false
	if request("tools/list", `{}`).Code != 401 {
		t.Fatal("removed binding still authorized")
	}
	bound, enabled = true, false
	if request("tools/list", `{}`).Code != 401 {
		t.Fatal("disabled service still authorized")
	}
}
