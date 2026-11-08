package api_test

import (
	"context"
	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/mcpserver"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityRegressions(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	aud := audit.NewMemory()
	svc := auth.NewService(users, auth.NewMemorySessionStore(), aud)
	if err := users.CreateRole(ctx, "PROBE", "", []auth.PermissionGrant{{Code: "employee.read", Scope: "OWN"}}); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("probe-password-123")
	u := &auth.User{ID: "probe-user", Username: "probe", PasswordHash: hash, Roles: []string{"PROBE"}}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	sess, err := svc.Login(ctx, u.Username, "probe-password-123", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	_, _, err = wf.UpsertWorkflowYAML(ctx, "id: private-workflow\nname: private-workflow\nversion: 1.0.0\nsteps:\n  - id: a\n    description: private\n", "patch")
	if err != nil {
		t.Fatal(err)
	}
	bus := eventbus.New(10)
	router := api.NewRouter(api.Deps{Auth: svc, Audit: aud, Bus: bus, WorkflowMCP: wf})
	t.Run("MCPReadRequiresPermission", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/workflow-mcp/workflows", nil)
		req.Header.Set("Authorization", "Bearer "+sess.Token)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != 403 {
			t.Fatalf("REST expected 403, got %d", rr.Code)
		}
		req = httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_workflows","arguments":{}}}`))
		req.Header.Set("Authorization", "Bearer "+sess.Token)
		rr = httptest.NewRecorder()
		(&mcpserver.Server{WF: wf, Auth: svc}).Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized || strings.Contains(rr.Body.String(), "private-workflow") {
			t.Fatalf("security regression: %s", rr.Body.String())
		}
	})
	t.Run("SSEHidesOtherUsersEvents", func(t *testing.T) {
		bus.Publish(ctx, eventbus.TypeJobStatus, map[string]string{"job_id": "other-user-job", "employee_id": "other-user-employee", "status": "RUNNING"})
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		req := httptest.NewRequest("GET", "/api/events", nil).WithContext(canceled)
		req.Header.Set("Authorization", "Bearer "+sess.Token)
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "other-user-job") {
			t.Fatalf("security regression: %s", rr.Body.String())
		}
	})
	t.Run("RemovedRolesLoseAuthorization", func(t *testing.T) {
		if err := users.CreateRole(ctx, "NOACCESS", "", nil); err != nil {
			t.Fatal(err)
		}
		if err := users.SetRoles(ctx, u.ID, []string{"NOACCESS"}); err != nil {
			t.Fatal(err)
		}
		current, err := svc.Authenticate(ctx, sess.Token)
		if err != nil {
			t.Fatal(err)
		}
		if len(current.Roles) != 1 || current.Roles[0] != "NOACCESS" {
			t.Fatalf("security regression: %v", current.Roles)
		}
		if err := svc.Authorize(ctx, current, "employee.read"); err == nil {
			t.Fatal(err)
		}
	})
}
