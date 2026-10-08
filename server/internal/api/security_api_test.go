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
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
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
	code, ws := doJSON(t, h, http.MethodPost, "/api/workspaces", tok, map[string]string{
		"workstation_id": "WS-1", "path": "/repo",
	})
	if code != 201 {
		t.Fatal(ws)
	}
	wsID, _ := ws["id"].(string)
	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{
		"name": "Dev", "workstation_id": "WS-1", "workspace_id": wsID,
	})
	if code != 201 {
		t.Fatal(emp)
	}
	code, j := doJSON(t, h, http.MethodPost, "/api/jobs", tok, map[string]any{
		"employee_id": emp["id"], "workspace_id": wsID, "prompt": "push", "idempotency_key": "p1",
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

func TestSecurityAPI_TOTPReEnrollAndDisableAndAuditFilter(t *testing.T) {
	h, tok, _ := setupM7(t)

	// 1. 首次 enroll (未绑定时)，缺少密码应返回 403
	code, resp := doJSON(t, h, http.MethodPost, "/api/auth/totp/enroll", tok, nil)
	if code != http.StatusForbidden {
		t.Fatalf("first enroll without password expected 403, got %d: %v", code, resp)
	}

	// 1.1 首次 enroll，输入正确密码 -> 返回待激活 secret (201)
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/enroll", tok, map[string]string{"password": "admin123"})
	if code != http.StatusCreated {
		t.Fatalf("first enroll with password expected 201, got %d: %v", code, resp)
	}
	sec1, _ := resp["secret"].(string)
	if sec1 == "" {
		t.Fatalf("expected secret in response: %v", resp)
	}

	// 1.2 首次激活 confirm: 提交扫码后的 6 位 TOTP
	otp0, _ := totp.CodeAt(sec1, time.Now())
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/confirm", tok, map[string]string{"totp": otp0})
	if code != http.StatusOK {
		t.Fatalf("confirm totp expected 200, got %d: %v", code, resp)
	}

	// 2. 已经开启 TOTP 后，缺少密码或 totp 重新 enroll -> 应返回 403 Forbidden
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/enroll", tok, map[string]string{"password": "admin123"})
	if code != http.StatusForbidden {
		t.Fatalf("re-enroll without totp expected 403, got %d: %v", code, resp)
	}

	// 3. 带错误 totp -> 应返回 403 Forbidden
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/enroll", tok, map[string]string{"password": "admin123", "totp": "000000"})
	if code != http.StatusForbidden {
		t.Fatalf("re-enroll with bad totp expected 403, got %d: %v", code, resp)
	}

	// 4. 同时带正确管理员密码与当前有效 totp -> 应成功重新生成 (201)
	otp1, _ := totp.CodeAt(sec1, time.Now())
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/enroll", tok, map[string]string{"password": "admin123", "totp": otp1})
	if code != http.StatusCreated {
		t.Fatalf("re-enroll with valid password and totp expected 201, got %d: %v", code, resp)
	}
	sec2, _ := resp["secret"].(string)
	if sec2 == "" || sec2 == sec1 {
		t.Fatalf("expected new secret after re-enroll, got sec2=%s, sec1=%s", sec2, sec1)
	}

	// 4.1 激活新 secret
	otp2, _ := totp.CodeAt(sec2, time.Now())
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/confirm", tok, map[string]string{"totp": otp2})
	if code != http.StatusOK {
		t.Fatalf("confirm new totp expected 200, got %d: %v", code, resp)
	}

	// 5. 尝试关闭 TOTP，缺少密码或口令 -> 应返回 403
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/disable", tok, map[string]string{"password": "admin123"})
	if code != http.StatusForbidden {
		t.Fatalf("disable totp without totp expected 403, got %d: %v", code, resp)
	}

	// 6. 同时带管理员密码与当前有效 TOTP 口令关闭 TOTP -> 应成功 (200)
	otp3, _ := totp.CodeAt(sec2, time.Now())
	code, resp = doJSON(t, h, http.MethodPost, "/api/auth/totp/disable", tok, map[string]string{"password": "admin123", "totp": otp3})
	if code != http.StatusOK {
		t.Fatalf("disable totp with valid password and totp expected 200, got %d: %v", code, resp)
	}

	// 校验 TOTP 状态已关闭
	code, stResp := doJSON(t, h, http.MethodGet, "/api/auth/totp", tok, nil)
	if code != http.StatusOK || stResp["enabled"] != false {
		t.Fatalf("expected totp disabled, got code=%d, resp=%v", code, stResp)
	}

	// 7. 测试审计日志模糊检索
	// 检索 "totp" 关键字 -> 应检索到 totp.enroll, totp.re_enroll, totp.disable
	code, auditResp := doJSON(t, h, http.MethodGet, "/api/audit?q=totp", tok, nil)
	if code != http.StatusOK {
		t.Fatalf("audit search expected 200, got %d: %v", code, resp)
	}
	items, _ := auditResp["items"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected audit items matching 'totp', got 0")
	}
}
