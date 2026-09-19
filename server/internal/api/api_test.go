package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/workspace"
)

func setupAPI(t *testing.T) (http.Handler, string) {
	t.Helper()
	auditor := audit.NewMemory()
	bus := eventbus.New(100)
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Admin"); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	presence := reliability.NewPresence(5, 15)
	empStore := employee.NewMemoryStore()
	empSvc := employee.NewService(empStore, auditor, bus)
	wsSvc := workspace.NewService(workspace.NewMemoryStore(), auditor)
	wsSvc.SetBinder(workspace.EmployeeBridge{
		GetByWorkspace: func(ctx context.Context, wsID string) (string, error) {
			e, _ := empStore.FindByWorkspace(ctx, wsID)
			if e == nil {
				return "", nil
			}
			return e.ID, nil
		},
		SetWorkspace: func(ctx context.Context, empID, wsID string) error {
			e, err := empStore.Get(ctx, empID)
			if err != nil {
				return err
			}
			e.WorkspaceID = wsID
			return empStore.Save(ctx, e)
		},
	})
	h := api.NewRouter(api.Deps{
		Auth:         authSvc,
		Enrollment:   enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor),
		CA:           ca,
		Employees:    empSvc,
		Workspaces:   wsSvc,
		Workstations: workstation.NewService(ca, presence, nil),
		Sessions:     session.NewService(session.NewMemoryStore(), auditor, bus),
		Jobs:         job.NewService(job.NewMemoryStore(), auditor, bus),
		Messages:     message.NewService(message.NewMemoryStore(), auditor),
		Bus:          bus,
		Audit:        auditor,
	})
	token := login(t, h)
	return h, token
}

func login(t *testing.T, h http.Handler) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "admin123"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body)))
	if rr.Code != 200 {
		t.Fatalf("login %d %s", rr.Code, rr.Body.String())
	}
	var out struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out.Token == "" {
		t.Fatal("无 token")
	}
	return out.Token
}

func doJSON(t *testing.T, h http.Handler, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestAPIAuthRequired(t *testing.T) {
	h, _ := setupAPI(t)
	code, _ := doJSON(t, h, http.MethodGet, "/api/employees", "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("code=%d", code)
	}
}

func TestAPIEmployeeJobSessionMessageFlow(t *testing.T) {
	h, tok := setupAPI(t)

	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{"name": "Dev"})
	if code != 201 {
		t.Fatalf("%d %v", code, emp)
	}
	empID := emp["id"].(string)

	code, ws := doJSON(t, h, http.MethodPost, "/api/workspaces", tok, map[string]string{
		"workstation_id": "WSN-demo", "path": "/src", "repository": "demo", "branch": "main",
	})
	if code != 201 {
		t.Fatal(ws)
	}
	wsID := ws["id"].(string)
	code, _ = doJSON(t, h, http.MethodPost, "/api/workspaces/"+wsID+"/bind", tok, map[string]string{"employee_id": empID})
	if code != 200 {
		t.Fatal(code)
	}

	code, sess := doJSON(t, h, http.MethodPost, "/api/sessions", tok, map[string]string{
		"employee_id": empID, "workspace_id": wsID, "provider": "cursor",
	})
	if code != 201 {
		t.Fatal(sess)
	}
	sessID := sess["id"].(string)
	code, _ = doJSON(t, h, http.MethodPost, "/api/sessions/"+sessID+"/transition", tok, map[string]string{"status": "READY"})
	if code != 200 {
		t.Fatal(code)
	}

	code, jobResp := doJSON(t, h, http.MethodPost, "/api/jobs", tok, map[string]any{
		"employee_id": empID, "workspace_id": wsID, "session_id": sessID,
		"prompt": "fix", "idempotency_key": "idem-1", "timeout_sec": 60,
	})
	if code != 201 {
		t.Fatal(jobResp)
	}
	jobObj := jobResp["job"].(map[string]any)
	jobID := jobObj["id"].(string)
	code, jobResp2 := doJSON(t, h, http.MethodPost, "/api/jobs", tok, map[string]any{
		"employee_id": empID, "prompt": "fix", "idempotency_key": "idem-1",
	})
	if code != 200 || jobResp2["idempotent"] != true {
		t.Fatalf("幂等: %d %v", code, jobResp2)
	}

	for _, st := range []string{"QUEUED", "ASSIGNED", "STARTING", "RUNNING"} {
		code, _ = doJSON(t, h, http.MethodPost, "/api/jobs/"+jobID+"/transition", tok, map[string]string{"status": st})
		if code != 200 {
			t.Fatalf("transition %s: %d", st, code)
		}
	}
	code, tl := doJSON(t, h, http.MethodGet, "/api/jobs/"+jobID+"/events", tok, nil)
	if code != 200 || tl["items"] == nil {
		t.Fatal(tl)
	}

	code, msg := doJSON(t, h, http.MethodPost, "/api/messages", tok, map[string]string{
		"sender_type": "EMPLOYEE", "sender_id": empID,
		"receiver_type": "EMPLOYEE", "receiver_id": "EMP-OTHER",
		"content": "ping",
	})
	if code != 201 {
		t.Fatal(msg)
	}

	code, dash := doJSON(t, h, http.MethodGet, "/api/dashboard", tok, nil)
	if code != 200 || dash["employees"].(float64) < 1 {
		t.Fatal(dash)
	}
	code, auditResp := doJSON(t, h, http.MethodGet, "/api/audit?limit=20", tok, nil)
	if code != 200 {
		t.Fatal(auditResp)
	}
	items := auditResp["items"].([]any)
	if len(items) == 0 {
		t.Fatal("审计为空")
	}
}

func TestAPISSEReceivesJobEvent(t *testing.T) {
	h, tok := setupAPI(t)
	_, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{"name": "S"})
	empID := emp["id"].(string)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/api/events", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	doJSON(t, h, http.MethodPost, "/api/jobs", tok, map[string]any{
		"employee_id": empID, "prompt": "x", "idempotency_key": "sse-1",
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rr.Body.String(), "job.status") {
			cancel()
			<-done
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("SSE 未收到 job.status: %s", rr.Body.String())
}

func TestAPIUnauthorizedCannotWrite(t *testing.T) {
	h, _ := setupAPI(t)
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("viewer", "viewer123", "V")
	// 直接用 ADMIN token 测无 token 路径即可
	code, _ := doJSON(t, h, http.MethodPost, "/api/employees", "", map[string]string{"name": "x"})
	if code != http.StatusUnauthorized {
		t.Fatal(code)
	}
}
