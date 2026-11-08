package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestResourceReferenceAuthorization(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(10)
	users := auth.NewMemoryUserStore()
	grants := []auth.PermissionGrant{}
	for _, code := range []string{"employee.write", "workspace.write", "job.write"} {
		grants = append(grants, auth.PermissionGrant{Code: code, Scope: "OWN"})
	}
	if err := users.CreateRole(ctx, "LIMITED", "", grants); err != nil {
		t.Fatal(err)
	}
	hash, _ := auth.HashPassword("audit-password-123")
	u := &auth.User{ID: "caller", Username: "caller", PasswordHash: hash, Status: auth.StatusActive, Roles: []string{"LIMITED"}}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(users, auth.NewMemorySessionStore(), aud)
	sess, err := a.Login(ctx, u.Username, "audit-password-123", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	es := employee.NewMemoryStore()
	emps := employee.NewService(es, aud, bus)
	own, _ := emps.Create(ctx, employee.CreateInput{Name: "own", OwnerUserID: u.ID, WorkstationID: "allowed"}, u.ID, "")
	victim, _ := emps.Create(ctx, employee.CreateInput{Name: "victim", OwnerUserID: "victim", WorkstationID: "allowed"}, "victim", "")
	ws := workspace.NewService(workspace.NewMemoryStore(), aud)
	ws.SetBinder(workspace.EmployeeBridge{
		GetByWorkspace: func(ctx context.Context, id string) (string, error) {
			e, err := es.FindByWorkspace(ctx, id)
			if e == nil {
				return "", err
			}
			return e.ID, err
		},
		SetWorkspace: func(ctx context.Context, id, wid string) error {
			e, err := es.Get(ctx, id)
			if err != nil {
				return err
			}
			e.WorkspaceID = wid
			return es.Save(ctx, e)
		},
	})
	ownWS, _ := ws.Create(ctx, workspace.CreateInput{WorkstationID: "allowed", Path: "/safe", EmployeeID: own.ID}, u.ID, "")
	foreignWS, _ := ws.Create(ctx, workspace.CreateInput{WorkstationID: "private", Path: "/private", EmployeeID: victim.ID}, "victim", "")
	shares := workstation.NewMemoryShare()
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "allowed", CreatedBy: u.ID})
	h := api.NewRouter(api.Deps{Auth: a, Audit: aud, Employees: emps, Workspaces: ws, WSShare: shares, Jobs: job.NewService(job.NewMemoryStore(), aud, bus)})
	for _, tt := range []struct {
		name, method, path string
		body               any
	}{
		{"employee node", "PATCH", "/api/employees/" + own.ID, map[string]string{"workstation_id": "private"}},
		{"employee workspace", "PATCH", "/api/employees/" + own.ID, map[string]string{"workspace_id": foreignWS.ID}},
		{"workspace node", "PATCH", "/api/workspaces/" + ownWS.ID, map[string]string{"workstation_id": "private"}},
		{"victim binding", "POST", "/api/workspaces/" + ownWS.ID + "/bind", map[string]string{"employee_id": victim.ID}},
		{"creation binding", "POST", "/api/workspaces", map[string]string{"workstation_id": "allowed", "path": "/safe", "employee_id": victim.ID}},
		{"foreign owner", "POST", "/api/employees", map[string]string{"name": "bad", "owner_user_id": "victim", "workstation_id": "allowed"}},
		{"foreign job workspace", "POST", "/api/jobs", map[string]string{"employee_id": own.ID, "workspace_id": foreignWS.ID, "prompt": "test"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, body := doJSON(t, h, tt.method, tt.path, sess.Token, tt.body)
			if code != http.StatusForbidden {
				t.Fatalf("want 403, got %d %v", code, body)
			}
		})
	}
	got, _ := emps.Get(ctx, own.ID)
	v, _ := emps.Get(ctx, victim.ID)
	w, _ := ws.Get(ctx, ownWS.ID)
	if got.WorkstationID != "allowed" || v.WorkspaceID != "" || w.WorkstationID != "allowed" {
		t.Fatal("拒绝请求产生了修改")
	}
	code, body := doJSON(t, h, "POST", "/api/workspaces/"+ownWS.ID+"/bind", sess.Token, map[string]string{"employee_id": own.ID})
	if code != 200 {
		t.Fatal(code, body)
	}
}
