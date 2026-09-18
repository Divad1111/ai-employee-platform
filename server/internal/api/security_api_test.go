package api_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/totp"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/workspace"
)

func setupM7(t *testing.T) (http.Handler, string, *approval.Service) {
	t.Helper()
	auditor := audit.NewMemory()
	bus := eventbus.New(50)
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin123", "Admin")
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	vault, _ := secret.NewMemoryVault()
	empSvc := employee.NewService(employee.NewMemoryStore(), auditor, bus)
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, bus)
	permStore := permission.NewMemoryStore()
	_ = permission.EnsureDefault(permStore)
	eng := permission.NewEngine(permStore, auditor)
	apr := approval.New(approval.NewMemoryStore(), approval.NewMemoryTOTP(), vault, jobSvc, eng, auditor, bus)
	h := api.NewRouter(api.Deps{
		Auth: authSvc, Enrollment: enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor), CA: ca,
		Employees: empSvc, Workspaces: workspace.NewService(workspace.NewMemoryStore(), auditor),
		Workstations: workstation.NewService(ca, presence, nil),
		Sessions:     session.NewService(session.NewMemoryStore(), auditor, bus),
		Jobs:         jobSvc, Messages: message.NewService(message.NewMemoryStore(), auditor),
		Bus: bus, Audit: auditor, Secrets: vault,
		Permission: eng, PermissionStore: permStore, Approvals: apr,
	})
	return h, login(t, h), apr
}

func TestStepUpRequiredForDelete(t *testing.T) {
	h, tok, _ := setupM7(t)
	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{"name": "X"})
	if code != 201 {
		t.Fatal(emp)
	}
	id := emp["id"].(string)
	code, resp := doJSON(t, h, http.MethodDelete, "/api/employees/"+id, tok, nil)
	if code != 403 {
		t.Fatalf("未 step-up 应 403: %d %v", code, resp)
	}
	code, su := doJSON(t, h, http.MethodPost, "/api/auth/step-up", tok, map[string]string{"password": "admin123"})
	if code != 200 {
		t.Fatal(code, su)
	}
	code, del := doJSON(t, h, http.MethodDelete, "/api/employees/"+id, tok, nil)
	if code != 200 {
		t.Fatalf("step-up 后应删除: %d %v", code, del)
	}
}

func TestPermissionEvaluateGitPushNeedsTOTP(t *testing.T) {
	h, tok, apr := setupM7(t)
	ctx := context.Background()
	code, out := doJSON(t, h, http.MethodPost, "/api/permission/evaluate", tok, map[string]string{
		"actor_type": "EMPLOYEE", "actor_id": "EMP-1", "action": permission.ActionGitPush,
	})
	if code != 200 {
		t.Fatal(out)
	}
	dec := out["decision"].(map[string]any)
	if dec["effect"] != permission.EffectDeny {
		t.Fatal(dec)
	}
	code, me := doJSON(t, h, http.MethodGet, "/api/auth/me", tok, nil)
	if code != 200 {
		t.Fatal(me)
	}
	uid, _ := me["id"].(string)
	if uid == "" {
		t.Fatal(me)
	}
	sec, _, err := apr.EnrollTOTP(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{
		"name": "Dev", "workstation_id": "WS-1", "workspace_id": "W1",
	})
	if code != 201 {
		t.Fatal(emp)
	}
	code, j := doJSON(t, h, http.MethodPost, "/api/jobs", tok, map[string]any{
		"employee_id": emp["id"], "workspace_id": "W1", "prompt": "push", "idempotency_key": "p1",
	})
	if code != 201 {
		t.Fatal(j)
	}
	jobObj := j["job"].(map[string]any)
	jobID := jobObj["id"].(string)
	for _, st := range []string{"QUEUED", "ASSIGNED", "STARTING", "RUNNING"} {
		doJSON(t, h, http.MethodPost, "/api/jobs/"+jobID+"/transition", tok, map[string]string{"status": st})
	}
	code, out = doJSON(t, h, http.MethodPost, "/api/permission/evaluate", tok, map[string]any{
		"actor_type": "EMPLOYEE", "actor_id": emp["id"], "action": permission.ActionGitPush, "job_id": jobID,
	})
	if code != 200 {
		t.Fatal(out)
	}
	dec = out["decision"].(map[string]any)
	if dec["effect"] != permission.EffectAsk {
		t.Fatal(dec, out)
	}
	aprMap, _ := out["approval"].(map[string]any)
	if aprMap == nil {
		t.Fatal(out)
	}
	aprID := aprMap["id"].(string)
	otp, _ := totp.CodeAt(sec, time.Now())
	code, ap := doJSON(t, h, http.MethodPost, "/api/approvals/"+aprID+"/approve", tok, map[string]string{"totp": otp})
	if code != 200 {
		t.Fatal(ap)
	}
}
