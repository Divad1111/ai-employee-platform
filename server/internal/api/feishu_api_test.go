package api_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/enrollment"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func setupM6(t *testing.T) (http.Handler, string, *feishu.MemorySender, *reliability.Presence) {
	t.Helper()
	auditor := audit.NewMemory()
	bus := eventbus.New(50)
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin123", "Admin")
	adminUser, _ := users.FindByUsername(context.Background(), "admin")
	if adminUser != nil {
		_ = users.SetRoles(context.Background(), adminUser.ID, []string{"SUPER_ADMIN", "ADMIN"})
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	vault, _ := secret.NewMemoryVault()
	empStore := employee.NewMemoryStore()
	empSvc := employee.NewService(empStore, auditor, bus)
	wsSvc := workspace.NewService(workspace.NewMemoryStore(), auditor)
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, bus)
	msgSvc := message.NewService(message.NewMemoryStore(), auditor)
	wss := workstation.NewService(ca, presence, nil)
	sender := &feishu.MemorySender{}
	fs := feishu.NewService(vault)
	fs.Sender = sender
	fs.SetConfig(feishu.Config{VerificationToken: "vt", Enabled: true})
	notify := notification.New(fs, bus, jobSvc)
	sched := scheduler.New(jobSvc, empSvc, wss, presence, nil)
	bridge := &feishu.Bridge{Employees: empSvc, Jobs: jobSvc, Messages: msgSvc, Scheduler: sched, Notify: notify, Feishu: fs}
	bridge.Wire()
	h := api.NewRouter(api.Deps{
		Auth: authSvc, Enrollment: enrollment.NewService(enrollment.NewMemoryStore(), ca, auditor), CA: ca,
		Employees: empSvc, Workspaces: wsSvc, Workstations: wss, Sessions: session.NewService(session.NewMemoryStore(), auditor, bus),
		Jobs: jobSvc, Messages: msgSvc, Bus: bus, Audit: auditor,
		Feishu: fs, Scheduler: sched, Notify: notify, Secrets: vault,
	})
	tok := login(t, h)
	return h, tok, sender, presence
}

func TestFeishuWebhookChallengeAndJob(t *testing.T) {
	h, tok, sender, presence := setupM6(t)
	// 建 Employee + 绑定
	code, ws := doJSON(t, h, http.MethodPost, "/api/workspaces", tok, map[string]string{
		"workstation_id": "WS-1", "path": "/repo",
	})
	if code != 201 {
		t.Fatal(ws)
	}
	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{
		"name": "Dev", "workstation_id": "WS-1", "workspace_id": ws["id"].(string),
	})
	if code != 201 {
		t.Fatal(emp)
	}
	empID := emp["id"].(string)
	if emp["owner_user_id"] == nil || emp["owner_user_id"] == "" {
		t.Fatalf("员工缺少归属用户: %#v", emp)
	}
	doJSON(t, h, http.MethodPost, "/api/integrations/feishu/bindings", tok, map[string]string{
		"employee_id": empID, "feishu_bot_alias": "dev", "feishu_open_id": "ou_1",
	})
	presence.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	wssMeta := h // ensure registered via ensure on schedule path - register
	_ = wssMeta
	// URL challenge
	body := []byte(`{"type":"url_verification","token":"vt","challenge":"c123"}`)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/integrations/feishu/events", bytes.NewReader(body)))
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	// 验证未携带任何合法签名和 token 的恶意请求被拦截（401 Unauthorized）
	unauthBody := []byte(`{
	  "header":{"event_id":"eid-unauth","event_type":"im.message.receive_v1"},
	  "event":{"message":{"chat_id":"oc_1","message_id":"om_unauth","content":"{\"text\":\"@dev rm -rf /\"}"}}
	}`)
	unauthRR := httptest.NewRecorder()
	h.ServeHTTP(unauthRR, httptest.NewRequest(http.MethodPost, "/api/integrations/feishu/events", bytes.NewReader(unauthBody)))
	if unauthRR.Code != 401 {
		t.Fatalf("预期未授权请求应返回 401，实际返回 %d: %s", unauthRR.Code, unauthRR.Body.String())
	}

	// 携带正确 Token 的合法消息事件
	msgBody := []byte(`{
	  "header":{"event_id":"eid-1","event_type":"im.message.receive_v1","token":"vt"},
	  "event":{"message":{"chat_id":"oc_1","message_id":"om_1","content":"{\"text\":\"@dev fix bug\"}"},
	           "sender":{"sender_id":{"open_id":"ou_1"}}}
	}`)
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/feishu/events", bytes.NewReader(msgBody))
	h.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatal(rr.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out["job_id"] == "" || out["duplicate"] == true {
		t.Fatal(out)
	}
	jobID := out["job_id"].(string)
	// 重复 event 幂等
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/integrations/feishu/events", bytes.NewReader(msgBody)))
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if out["duplicate"] != true {
		t.Fatal(out)
	}
	// Scheduler 可能已到 STARTING；推进到 SUCCESS 应飞书回复
	code, cur := doJSON(t, h, http.MethodGet, "/api/jobs/"+jobID, tok, nil)
	if code != 200 {
		t.Fatal(cur)
	}
	st := cur["status"].(string)
	steps := []string{}
	switch st {
	case "CREATED":
		steps = []string{"QUEUED", "ASSIGNED", "STARTING", "RUNNING", "SUCCESS"}
	case "QUEUED":
		steps = []string{"ASSIGNED", "STARTING", "RUNNING", "SUCCESS"}
	case "ASSIGNED":
		steps = []string{"STARTING", "RUNNING", "SUCCESS"}
	case "STARTING":
		steps = []string{"RUNNING", "SUCCESS"}
	case "RUNNING":
		steps = []string{"SUCCESS"}
	default:
		t.Fatalf("unexpected status %s", st)
	}
	for _, s := range steps {
		code, resp := doJSON(t, h, http.MethodPost, "/api/jobs/"+jobID+"/transition", tok, map[string]string{"status": s})
		if code != 200 {
			t.Fatalf("→%s: %d %v", s, code, resp)
		}
	}
	if len(sender.Sent) < 1 {
		t.Fatal("缺少飞书回复")
	}
}

func TestFeishuSignatureReject(t *testing.T) {
	h, _, _, _ := setupM6(t)
	body := `{"x":1}`
	_ = sha256.Sum256([]byte("1" + "n" + "vt" + body))
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/feishu/events", bytes.NewReader([]byte(body)))
	req.Header.Set("X-Lark-Request-Timestamp", "1")
	req.Header.Set("X-Lark-Request-Nonce", "n")
	req.Header.Set("X-Lark-Signature", "deadbeef")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatal(rr.Code, rr.Body.String())
	}
}

func TestSecretNotEchoed(t *testing.T) {
	h, tok, _, _ := setupM6(t)
	code, ref := doJSON(t, h, http.MethodPost, "/api/secrets", tok, map[string]string{
		"name": "k", "value": "plain-secret",
	})
	if code != 201 {
		t.Fatal(ref)
	}
	if _, ok := ref["value"]; ok {
		t.Fatal("不应回显明文")
	}
	// 改系统配置需 step-up
	code, _ = doJSON(t, h, http.MethodPost, "/api/auth/step-up", tok, map[string]string{"password": "admin123"})
	if code != 200 {
		t.Fatal("step-up", code)
	}
	code, cfg := doJSON(t, h, http.MethodPut, "/api/integrations/feishu/config", tok, map[string]any{
		"app_id": "cli_x", "app_secret": "s3cret", "verification_token": "vt2", "enabled": true,
	})
	if code != 200 {
		t.Fatal(cfg)
	}
	if cfg["app_secret"] != nil {
		t.Fatal("配置不应含明文 secret")
	}
	if cfg["app_secret_ref"] == "" {
		t.Fatal("应有引用")
	}
}

func TestFeishuBindingListScopedToOwner(t *testing.T) {
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin12345", "Admin"); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	h := api.NewRouter(api.Deps{
		Auth:      authSvc,
		Audit:     auditor,
		Employees: employee.NewService(employee.NewMemoryStore(), auditor, nil),
		Feishu:    feishu.NewService(vault),
	})
	login := func(user, pass string) string {
		t.Helper()
		code, resp := doJSON(t, h, http.MethodPost, "/api/auth/login", "", map[string]string{"username": user, "password": pass})
		if code != http.StatusOK {
			t.Fatalf("login %s: %d %v", user, code, resp)
		}
		return resp["token"].(string)
	}
	adminTok := login("admin", "admin12345")
	code, alice := doJSON(t, h, http.MethodPost, "/api/users", adminTok, map[string]any{
		"username": "alice", "password": "alice12345", "roles": []string{"USER"},
	})
	if code != http.StatusCreated {
		t.Fatalf("create alice: %d %v", code, alice)
	}
	aliceID := alice["id"].(string)
	code, mine := doJSON(t, h, http.MethodPost, "/api/employees", adminTok, map[string]string{"name": "管理员的员工"})
	if code != http.StatusCreated {
		t.Fatalf("create mine: %d %v", code, mine)
	}
	code, hers := doJSON(t, h, http.MethodPost, "/api/employees", adminTok, map[string]any{
		"name": "Alice 的员工", "owner_user_id": aliceID,
	})
	if code != http.StatusCreated {
		t.Fatalf("create hers: %d %v", code, hers)
	}
	for _, b := range []map[string]string{
		{"employee_id": mine["id"].(string), "feishu_bot_alias": "adminbot"},
		{"employee_id": hers["id"].(string), "feishu_bot_alias": "alicebot"},
	} {
		code, body := doJSON(t, h, http.MethodPost, "/api/integrations/feishu/bindings", adminTok, b)
		if code != http.StatusOK {
			t.Fatalf("bind %s: %d %v", b["feishu_bot_alias"], code, body)
		}
	}
	aliceTok := login("alice", "alice12345")
	code, listed := doJSON(t, h, http.MethodGet, "/api/integrations/feishu/bindings", aliceTok, nil)
	if code != http.StatusOK {
		t.Fatalf("list: %d %v", code, listed)
	}
	items, _ := listed["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("应只看到自己的绑定: %+v", listed["items"])
	}
	row, _ := items[0].(map[string]any)
	if row["employee_id"] != hers["id"] || row["feishu_bot_alias"] != "alicebot" {
		t.Fatalf("看到了别人的绑定: %+v", row)
	}
	code, denied := doJSON(t, h, http.MethodPost, "/api/integrations/feishu/bindings", aliceTok, map[string]string{
		"employee_id": mine["id"].(string), "feishu_bot_alias": "stolen",
	})
	if code != http.StatusForbidden {
		t.Fatalf("不能改别人的绑定: %d %v", code, denied)
	}
	code, denied = doJSON(t, h, http.MethodDelete, "/api/integrations/feishu/bindings?feishu_bot_alias=adminbot", aliceTok, nil)
	if code != http.StatusForbidden {
		t.Fatalf("不能删别人的绑定: %d %v", code, denied)
	}
	code, all := doJSON(t, h, http.MethodGet, "/api/integrations/feishu/bindings", adminTok, nil)
	if code != http.StatusOK {
		t.Fatal(all)
	}
	if n := len(all["items"].([]any)); n != 2 {
		t.Fatalf("管理员应看到全部绑定, got %d", n)
	}
}
