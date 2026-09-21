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

func setupM8(t *testing.T) (http.Handler, string, *approval.Service, *secret.Manager) {
	t.Helper()
	auditor := audit.NewMemory()
	bus := eventbus.New(50)
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin123", "Admin")
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	ca, _ := certca.NewDevAuthority()
	vault, _ := secret.NewMemoryVault()
	mgr := secret.NewManager(vault, secret.NewMemoryBindings(), auditor)
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, bus)
	permStore := permission.NewMemoryStore()
	_ = permission.EnsureDefault(permStore)
	eng := permission.NewEngine(permStore, auditor)
	apr := approval.New(approval.NewMemoryStore(), approval.NewMemoryTOTP(), vault, jobSvc, eng, auditor, bus)
	h := api.NewRouter(api.Deps{
		Auth: authSvc, Enrollment: enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor), CA: ca,
		Employees: employee.NewService(employee.NewMemoryStore(), auditor, bus),
		Workspaces:   workspace.NewService(workspace.NewMemoryStore(), auditor),
		Workstations: workstation.NewService(ca, reliability.NewPresence(5, 15), nil),
		Sessions:     session.NewService(session.NewMemoryStore(), auditor, bus),
		Jobs: jobSvc, Messages: message.NewService(message.NewMemoryStore(), auditor),
		Bus: bus, Audit: auditor, Secrets: vault, SecretMgr: mgr,
		Permission: eng, PermissionStore: permStore, Approvals: apr,
	})
	return h, login(t, h), apr, mgr
}

func TestSecretCRUDNoPlaintext(t *testing.T) {
	h, tok, _, _ := setupM8(t)
	code, ref := doJSON(t, h, http.MethodPost, "/api/secrets", tok, map[string]string{
		"name": "npm", "value": "npm_secret_xyz", "description": "npm token",
	})
	if code != 201 {
		t.Fatal(ref)
	}
	if ref["masked"] != "***" {
		t.Fatal(ref)
	}
	if _, ok := ref["value"]; ok {
		t.Fatal("不得回显 value")
	}
	id := ref["id"].(string)
	code, list := doJSON(t, h, http.MethodGet, "/api/secrets", tok, nil)
	if code != 200 {
		t.Fatal(list)
	}
	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{"name": "E"})
	if code != 201 {
		t.Fatal(emp)
	}
	code, b := doJSON(t, h, http.MethodPost, "/api/secrets/bindings", tok, map[string]string{
		"employee_id": emp["id"].(string), "secret_id": id, "purpose": "NPM_TOKEN",
	})
	if code != 201 {
		t.Fatal(b)
	}
	code, res := doJSON(t, h, http.MethodPost, "/api/employees/"+emp["id"].(string)+"/secrets/resolve", tok, nil)
	if code != 200 {
		t.Fatal(res)
	}
	items := res["items"].([]any)
	if len(items) != 1 {
		t.Fatal(res)
	}
	item := items[0].(map[string]any)
	if item["value"] != nil {
		t.Fatal("resolve API 不得回传明文")
	}
	code, aud := doJSON(t, h, http.MethodGet, "/api/audit?prefix=secret.&limit=20", tok, nil)
	if code != 200 {
		t.Fatal(aud)
	}
}

func TestRotateRequiresTOTP(t *testing.T) {
	h, tok, apr, _ := setupM8(t)
	ctx := context.Background()
	code, ref := doJSON(t, h, http.MethodPost, "/api/secrets", tok, map[string]string{
		"name": "cred", "value": "old",
	})
	if code != 201 {
		t.Fatal(ref)
	}
	id := ref["id"].(string)
	doJSON(t, h, http.MethodPost, "/api/auth/step-up", tok, map[string]string{"password": "admin123"})
	code, deny := doJSON(t, h, http.MethodPost, "/api/secrets/"+id+"/rotate", tok, map[string]string{"value": "new"})
	if code != 403 {
		t.Fatalf("无 TOTP 应 403: %d %v", code, deny)
	}
	code, me := doJSON(t, h, http.MethodGet, "/api/auth/me", tok, nil)
	uid := me["id"].(string)
	sec, _, err := apr.EnrollTOTP(ctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	otp, _ := totp.CodeAt(sec, time.Now())
	doJSON(t, h, http.MethodPost, "/api/auth/step-up", tok, map[string]string{"password": "admin123", "totp": otp})
	otp2, _ := totp.CodeAt(sec, time.Now())
	code, ok := doJSON(t, h, http.MethodPost, "/api/secrets/"+id+"/rotate", tok, map[string]string{
		"value": "new-secret", "totp": otp2,
	})
	if code != 200 {
		t.Fatal(ok)
	}
}

func TestDeleteSecret(t *testing.T) {
	h, tok, _, _ := setupM8(t)
	code, ref := doJSON(t, h, http.MethodPost, "/api/secrets", tok, map[string]string{
		"name": "delete-me", "value": "secret-value",
	})
	if code != 201 {
		t.Fatal(ref)
	}
	id := ref["id"].(string)

	// 1. 未 step-up 删除 -> 应返回 403
	code, deny := doJSON(t, h, http.MethodDelete, "/api/secrets/"+id, tok, nil)
	if code != 403 {
		t.Fatalf("未 step-up 删除应返回 403, 实际: %d %v", code, deny)
	}

	// 2. step-up (输入管理员密码)
	code, su := doJSON(t, h, http.MethodPost, "/api/auth/step-up", tok, map[string]string{"password": "admin123"})
	if code != 200 {
		t.Fatalf("step-up 失败: %d %v", code, su)
	}

	// 3. step-up 后删除 -> 应成功 (200)
	code, del := doJSON(t, h, http.MethodDelete, "/api/secrets/"+id, tok, nil)
	if code != 200 {
		t.Fatalf("step-up 后删除失败: %d %v", code, del)
	}

	// 4. 再次获取 -> 应返回 404
	code, _ = doJSON(t, h, http.MethodGet, "/api/secrets/"+id, tok, nil)
	if code != 404 {
		t.Fatalf("删除后获取应 404, 实际: %d", code)
	}
}
