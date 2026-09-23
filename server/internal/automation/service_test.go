package automation

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/secret"
)

type memAudit struct {
	actions []string
}

func (m *memAudit) Log(_ context.Context, _, _, action, _, _ string, _ map[string]string) {
	m.actions = append(m.actions, action)
}

type fakeJobs struct {
	byID   map[string]*job.Job
	byIdem map[string]*job.Job
}

func (f *fakeJobs) Create(_ context.Context, in job.CreateInput, _, _ string) (*job.Job, bool, error) {
	if f.byID == nil {
		f.byID = map[string]*job.Job{}
		f.byIdem = map[string]*job.Job{}
	}
	if j, ok := f.byIdem[in.IdempotencyKey]; ok {
		return j, true, nil
	}
	j := &job.Job{
		ID: "JOB-" + in.IdempotencyKey, EmployeeID: in.EmployeeID, Prompt: in.Prompt,
		IdempotencyKey: in.IdempotencyKey, Status: job.StatusCreated, CreatedAt: time.Now().UTC(),
	}
	f.byID[j.ID] = j
	f.byIdem[in.IdempotencyKey] = j
	return j, false, nil
}

func (f *fakeJobs) Get(_ context.Context, id string) (*job.Job, error) {
	j, ok := f.byID[id]
	if !ok {
		return nil, job.ErrNotFound
	}
	return j, nil
}

type fakeSched struct{}

func (fakeSched) ScheduleJob(_ context.Context, _ string) (*job.Job, error) { return nil, nil }

func newTestSvc(t *testing.T) (*Service, *fakeJobs, *memAudit) {
	t.Helper()
	vault, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	jobs := &fakeJobs{}
	audit := &memAudit{}
	svc := New(NewMemoryStore(), jobs, fakeSched{}, vault, audit)
	return svc, jobs, audit
}

func TestCronDueAndFire(t *testing.T) {
	svc, _, audit := newTestSvc(t)
	ctx := context.Background()
	cfg, _ := json.Marshal(CronConfig{Expr: "* * * * *", Preset: "custom"})
	a, _, err := svc.Create(ctx, CreateInput{
		Name: "每分钟", TriggerType: TriggerCron,
		EmployeeID: "E1", Prompt: "ping", NotifyChatID: "oc_1", Timezone: "UTC",
		TriggerConfig: cfg,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 3, 15, 10, 30, 5, 0, time.UTC)
	svc.Tick(ctx, now)
	runs, _ := svc.ListRuns(ctx, a.ID, 10)
	if len(runs) != 1 || runs[0].Status != RunJobCreated {
		t.Fatalf("期望 1 次 job_created，got %+v", runs)
	}
	svc.Tick(ctx, now.Add(20*time.Second))
	runs2, _ := svc.ListRuns(ctx, a.ID, 10)
	if len(runs2) != 1 {
		t.Fatalf("同分钟应幂等，got %d", len(runs2))
	}
	found := false
	for _, act := range audit.actions {
		if act == "automation.trigger" {
			found = true
		}
	}
	if !found {
		t.Fatal("缺少 automation.trigger 审计")
	}
}

func TestParseCronPresets(t *testing.T) {
	if PresetCron("daily") != "0 9 * * *" {
		t.Fatal(PresetCron("daily"))
	}
	_, err := ParseCronConfig([]byte(`{"preset":"weekly"}`))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCalendarChainAdvanceAndStop(t *testing.T) {
	svc, jobs, _ := newTestSvc(t)
	ctx := context.Background()
	a, _, err := svc.Create(ctx, CreateInput{
		Name: "日历", TriggerType: TriggerCalendar, NotifyChatID: "oc_1", Timezone: "UTC",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	en := true
	items, err := svc.ReplaceCalendarItems(ctx, a.ID, "2026-03-15", []CalendarItemInput{
		{EmployeeID: "E1", Prompt: "step1", Enabled: &en},
		{EmployeeID: "E2", Prompt: "step2", Enabled: &en},
	}, "admin")
	if err != nil || len(items) != 2 {
		t.Fatalf("日历条目: %v %d", err, len(items))
	}
	now := time.Date(2026, 3, 15, 9, 0, 0, 0, time.UTC)
	svc.Tick(ctx, now)
	runs, _ := svc.ListRuns(ctx, a.ID, 10)
	if len(runs) != 1 {
		t.Fatalf("应只触发首条, got %d", len(runs))
	}
	j1 := jobs.byID[runs[0].JobID]
	j1.Status = job.StatusSuccess
	j1.Result = "ok"
	svc.OnJobTerminal(ctx, j1)
	runs2, _ := svc.ListRuns(ctx, a.ID, 10)
	if len(runs2) != 2 {
		t.Fatalf("SUCCESS 应推进下一条, got %d", len(runs2))
	}
	var step2Run *Run
	for _, r := range runs2 {
		if r.CalendarItemID == items[1].ID {
			step2Run = r
			break
		}
	}
	if step2Run == nil {
		t.Fatal("未找到 step2 run")
	}
	jFail := jobs.byID[step2Run.JobID]
	jFail.Status = job.StatusFailed
	jFail.Result = "boom"
	svc.OnJobTerminal(ctx, jFail)
	runs3, _ := svc.ListRuns(ctx, a.ID, 10)
	if len(runs3) != 2 {
		t.Fatalf("失败后不应再推进, got %d", len(runs3))
	}
}

func TestWebhookHMACBearerIPIdempotency(t *testing.T) {
	svc, _, audit := newTestSvc(t)
	ctx := context.Background()
	a, secrets, err := svc.Create(ctx, CreateInput{
		Name: "wh", TriggerType: TriggerWebhook,
		EmployeeID: "E1", Prompt: "from-hook", NotifyChatID: "oc_1",
		TriggerConfig: json.RawMessage(`{"auth_modes":["hmac","bearer"],"ip_allowlist":["127.0.0.1"],"rate_limit_per_min":10}`),
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var cfg WebhookConfig
	_ = json.Unmarshal(a.TriggerConfig, &cfg)
	hmacSec := secrets["hmac_secret"]
	if hmacSec == "" || cfg.PathToken == "" {
		t.Fatal("应返回 hmac_secret 与 path_token")
	}

	body := `{"hello":"world"}`
	ts := time.Now().UTC().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(hmacSec))
	_, _ = mac.Write([]byte(ts + "." + body))
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-AIE-Timestamp", ts)
	req.Header.Set("X-AIE-Signature", sig)
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req, []byte(body)); err == nil {
		t.Fatal("非白名单 IP 应失败")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req2.RemoteAddr = "127.0.0.1:9999"
	req2.Header.Set("X-AIE-Timestamp", ts)
	req2.Header.Set("X-AIE-Signature", sig)
	req2.Header.Set("X-Idempotency-Key", "k1")
	run1, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req2, []byte(body))
	if err != nil {
		t.Fatal(err)
	}

	req3 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req3.RemoteAddr = "127.0.0.1:9999"
	req3.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	req3.Header.Set("X-Idempotency-Key", "k1")
	run2, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req3, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if run1.ID != run2.ID {
		t.Fatal("相同幂等键应返回同一 run")
	}

	denied := false
	for _, act := range audit.actions {
		if act == "automation.webhook_denied" {
			denied = true
		}
	}
	if !denied {
		t.Fatal("期望 webhook_denied 审计")
	}
}
