package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/artifact"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/metrics"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/registry"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/workspace"
)

func setupM9(t *testing.T) (http.Handler, string, *audit.Memory, *auth.MemoryUserStore) {
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
	art := artifact.New(artifact.NewMemoryStore(), "")
	reg, _, err := registry.New(registry.NewMemoryStore(), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = reg.UpsertProvider(context.Background(), &registry.Provider{ID: "cursor", Name: "Cursor"})
	met := metrics.New()
	met.WorkstationOnline.Store(2)
	met.JobSuccess.Store(3)
	h := api.NewRouter(api.Deps{
		Auth: authSvc, Enrollment: enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor), CA: ca,
		Employees: employee.NewService(employee.NewMemoryStore(), auditor, bus),
		Workspaces:   workspace.NewService(workspace.NewMemoryStore(), auditor),
		Workstations: workstation.NewService(ca, reliability.NewPresence(5, 15), nil),
		Sessions:     session.NewService(session.NewMemoryStore(), auditor, bus),
		Jobs: jobSvc, Messages: message.NewService(message.NewMemoryStore(), auditor),
		Bus: bus, Audit: auditor, Secrets: vault, SecretMgr: mgr,
		Permission: eng, PermissionStore: permStore, Approvals: apr,
		Artifacts: art, Registry: reg, Metrics: met,
	})
	return h, login(t, h), auditor, users
}

func TestMetricsEndpointScrapable(t *testing.T) {
	h, _, _, _ := setupM9(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"aie_workstation_online",
		"aie_job_running",
		"aie_job_success_total",
		"aie_job_failed_total",
		"aie_active_sessions",
		"aie_acp_connection",
		"aie_job_duration_seconds",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("缺少指标 %s\n%s", want, body)
		}
	}
}

func TestSigningKeyPublic(t *testing.T) {
	h, _, _, _ := setupM9(t)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/providers/signing-key", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var out map[string]any
	jsonUnmarshal(rr.Body.Bytes(), &out)
	if out["alg"] != "ed25519" || out["public_key_b64"] == "" {
		t.Fatal(out)
	}
}

func TestAuditExportAndArchive(t *testing.T) {
	h, tok, auditor, users := setupM9(t)
	auditor.Log(context.Background(), "USER", "u1", "secret.access", "success", "", map[string]string{"secret_id": "sec-1"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/audit/export?prefix=secret.&limit=10", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Fatal(ct)
	}
	if !strings.Contains(rr.Body.String(), "secret.access") {
		t.Fatal(rr.Body.String())
	}
	// 普通 ADMIN 不可归档
	code, deny := doJSON(t, h, http.MethodPost, "/api/audit/archive", tok, map[string]int{"older_than_days": 1})
	if code != 403 {
		t.Fatalf("非 SUPER_ADMIN 应 403: %d %v", code, deny)
	}
	u, _ := users.FindByUsername(context.Background(), "admin")
	u.Roles = []string{"SUPER_ADMIN"}
	_ = users.Update(context.Background(), u)
	// 重新登录以带上新角色
	tok2 := login(t, h)
	code, ok := doJSON(t, h, http.MethodPost, "/api/audit/archive", tok2, map[string]int{"older_than_days": 3650})
	if code != 200 {
		t.Fatal(code, ok)
	}
}

func TestArtifactUploadDownloadHTTP(t *testing.T) {
	h, tok, _, _ := setupM9(t)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("job_id", "JOB-1")
	_ = w.WriteField("name", "result.json")
	_ = w.WriteField("type", "json")
	fw, err := w.CreateFormFile("file", "result.json")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte(`{"ok":true}`))
	_ = w.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/artifacts", &buf)
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 201 {
		t.Fatal(rr.Code, rr.Body.String())
	}
	var out map[string]any
	jsonUnmarshal(rr.Body.Bytes(), &out)
	art := out["artifact"].(map[string]any)
	id := art["id"].(string)
	if art["sha256"] == "" {
		t.Fatal(art)
	}

	// 去重第二次
	var buf2 bytes.Buffer
	w2 := multipart.NewWriter(&buf2)
	_ = w2.WriteField("job_id", "JOB-2")
	_ = w2.WriteField("name", "copy.json")
	fw2, _ := w2.CreateFormFile("file", "copy.json")
	_, _ = fw2.Write([]byte(`{"ok":true}`))
	_ = w2.Close()
	req2 := httptest.NewRequest(http.MethodPost, "/api/artifacts", &buf2)
	req2.Header.Set("Authorization", "Bearer "+tok)
	req2.Header.Set("Content-Type", w2.FormDataContentType())
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	var out2 map[string]any
	jsonUnmarshal(rr2.Body.Bytes(), &out2)
	if out2["deduplicated"] != true {
		t.Fatal(out2)
	}

	dl := httptest.NewRequest(http.MethodGet, "/api/artifacts/"+id+"/download", nil)
	dl.Header.Set("Authorization", "Bearer "+tok)
	rr3 := httptest.NewRecorder()
	h.ServeHTTP(rr3, dl)
	if rr3.Code != 200 {
		t.Fatal(rr3.Code, rr3.Body.String())
	}
	body, _ := io.ReadAll(rr3.Body)
	if string(body) != `{"ok":true}` {
		t.Fatal(string(body))
	}
	if rr3.Header().Get("X-SHA256") == "" {
		t.Fatal("缺 hash 头")
	}
}

func TestDeleteWorkstation_API(t *testing.T) {
	h, tok, _, _ := setupM9(t)

	// 1. 未 step-up 应 403
	req := httptest.NewRequest(http.MethodDelete, "/api/workstations/WS-del-1", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("未 step-up 应 403, got %d", rr.Code)
	}

	// 2. 执行 step-up
	suReq := httptest.NewRequest(http.MethodPost, "/api/auth/step-up", strings.NewReader(`{"password":"admin123"}`))
	suReq.Header.Set("Authorization", "Bearer "+tok)
	suReq.Header.Set("Content-Type", "application/json")
	suRR := httptest.NewRecorder()
	h.ServeHTTP(suRR, suReq)
	if suRR.Code != http.StatusOK {
		t.Fatalf("step-up 失败: %d %s", suRR.Code, suRR.Body.String())
	}

	// 3. step-up 后应成功删除
	req2 := httptest.NewRequest(http.MethodDelete, "/api/workstations/WS-del-1", nil)
	req2.Header.Set("Authorization", "Bearer "+tok)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("step-up 后应成功删除: %d %s", rr2.Code, rr2.Body.String())
	}
}

func jsonUnmarshal(b []byte, v any) {
	_ = json.Unmarshal(b, v)
}
