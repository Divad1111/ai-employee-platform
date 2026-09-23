package automation

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func signBody(secret, ts, body string) string {
	return computeHMAC(secret, ts, body)
}

func createWebhook(t *testing.T, svc *Service, cfgJSON string) (*Automation, map[string]string, WebhookConfig) {
	t.Helper()
	a, secrets, err := svc.Create(context.Background(), CreateInput{
		Name: "wh-sec", TriggerType: TriggerWebhook,
		EmployeeID: "E1", Prompt: "p", NotifyChatID: "oc_1",
		TriggerConfig: json.RawMessage(cfgJSON),
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	var cfg WebhookConfig
	_ = json.Unmarshal(a.TriggerConfig, &cfg)
	if cfg.PathToken == "" {
		t.Fatal("缺少 path_token")
	}
	return a, secrets, cfg
}

func TestVerifyHMAC_RejectsBadSignatureAndSkew(t *testing.T) {
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	sec := "super-secret"
	body := `{"a":1}`
	ts := now.Format(time.RFC3339)
	good := signBody(sec, ts, body)

	if err := verifyHMAC(sec, ts, good, body, now, 5*time.Minute); err != nil {
		t.Fatal("合法签名应通过", err)
	}
	if err := verifyHMAC(sec, ts, "deadbeef", body, now, 5*time.Minute); err == nil {
		t.Fatal("错误签名应拒绝")
	}
	if err := verifyHMAC(sec, ts, good, `{"a":2}`, now, 5*time.Minute); err == nil {
		t.Fatal("篡改 body 应拒绝")
	}
	if err := verifyHMAC("", ts, good, body, now, 5*time.Minute); err == nil {
		t.Fatal("空 secret 应拒绝")
	}
	if err := verifyHMAC(sec, "", good, body, now, 5*time.Minute); err == nil {
		t.Fatal("缺 timestamp 应拒绝")
	}
	oldTS := now.Add(-10 * time.Minute).Format(time.RFC3339)
	oldSig := signBody(sec, oldTS, body)
	if err := verifyHMAC(sec, oldTS, oldSig, body, now, 5*time.Minute); err == nil {
		t.Fatal("超过 skew 应拒绝")
	}
	// unix 秒应接受
	u := formatUnix(now.Unix())
	uSig := signBody(sec, u, body)
	if err := verifyHMAC(sec, u, uSig, body, now, 5*time.Minute); err != nil {
		t.Fatal("unix timestamp 应通过", err)
	}
	// 大小写不敏感
	if err := verifyHMAC(sec, ts, strings.ToUpper(good), body, now, 5*time.Minute); err != nil {
		t.Fatal("hex 大小写应通过", err)
	}
}

func formatUnix(n int64) string {
	if n <= 0 {
		return "0"
	}
	var b [32]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func TestVerifyBearer(t *testing.T) {
	tok := "tok-abc"
	if err := verifyBearer(tok, "Bearer "+tok); err != nil {
		t.Fatal(err)
	}
	if err := verifyBearer(tok, "bearer "+tok); err != nil {
		t.Fatal("Bearer 大小写应通过", err)
	}
	if err := verifyBearer(tok, tok); err != nil {
		t.Fatal("裸 token 应通过", err)
	}
	if err := verifyBearer(tok, "Bearer wrong"); err == nil {
		t.Fatal("错误 token 应拒绝")
	}
	if err := verifyBearer("", "Bearer x"); err == nil {
		t.Fatal("空期望应拒绝")
	}
	if err := verifyBearer(tok, ""); err == nil {
		t.Fatal("空头应拒绝")
	}
}

func TestIPAllowlistAndCIDR(t *testing.T) {
	if !ipAllowed("1.2.3.4", nil) {
		t.Fatal("空白名单应放行")
	}
	if !ipAllowed("10.0.0.5", []string{"10.0.0.5"}) {
		t.Fatal("精确匹配应放行")
	}
	if ipAllowed("10.0.0.6", []string{"10.0.0.5"}) {
		t.Fatal("非名单应拒绝")
	}
	if !ipAllowed("203.0.113.10", []string{"203.0.113.0/24"}) {
		t.Fatal("CIDR 内应放行")
	}
	if ipAllowed("203.0.114.1", []string{"203.0.113.0/24"}) {
		t.Fatal("CIDR 外应拒绝")
	}
}

func TestClientIPPrefersXFF(t *testing.T) {
	if got := clientIP("10.0.0.1:1234", "203.0.113.9, 10.0.0.1"); got != "203.0.113.9" {
		t.Fatalf("XFF 应取首个 IP, got %q", got)
	}
	if got := clientIP("127.0.0.1:9999", ""); got != "127.0.0.1" {
		t.Fatalf("RemoteAddr 解析失败: %q", got)
	}
}

func TestWebhookLimiter(t *testing.T) {
	l := newWebhookLimiter()
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if !l.allow("k", 3, now) {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if l.allow("k", 3, now) {
		t.Fatal("超限应拒绝")
	}
	// 一分钟后窗口滑动
	if !l.allow("k", 3, now.Add(61*time.Second)) {
		t.Fatal("窗口滑动后应放行")
	}
}

func TestWebhookUnknownPathAndDisabled(t *testing.T) {
	svc, _, audit := newTestSvc(t)
	ctx := context.Background()
	a, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer"],"rate_limit_per_min":60}`)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.RemoteAddr = "127.0.0.1:1"
	req.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	if _, _, err := svc.HandleWebhook(ctx, "no-such-token", req, []byte("{}")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("未知 path 应 NotFound, got %v", err)
	}

	off := false
	_, err := svc.Update(ctx, a.ID, UpdateInput{Enabled: &off}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req2.RemoteAddr = "127.0.0.1:1"
	req2.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req2, []byte("{}")); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("禁用规则应 Forbidden, got %v", err)
	}
	_ = audit
}

func TestWebhookHMACOnlyRejectsBearerAlone(t *testing.T) {
	svc, _, audit := newTestSvc(t)
	ctx := context.Background()
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["hmac"],"ip_allowlist":[],"rate_limit_per_min":60}`)
	if secrets["hmac_secret"] == "" {
		t.Fatal("应有 hmac_secret")
	}

	// 仅 Bearer、无 HMAC → 拒绝
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.RemoteAddr = "1.1.1.1:1"
	req.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req, []byte(`{}`)); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("HMAC-only 模式拒绝裸 Bearer, got %v", err)
	}

	body := `{"ok":true}`
	ts := time.Now().UTC().Format(time.RFC3339)
	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req2.RemoteAddr = "1.1.1.1:1"
	req2.Header.Set("X-AIE-Timestamp", ts)
	req2.Header.Set("X-AIE-Signature", signBody(secrets["hmac_secret"], ts, body))
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req2, []byte(body)); err != nil {
		t.Fatal("合法 HMAC 应通过", err)
	}

	foundAuthDeny := false
	for _, act := range audit.actions {
		if act == "automation.webhook_denied" {
			foundAuthDeny = true
		}
	}
	if !foundAuthDeny {
		t.Fatal("鉴权失败应写 webhook_denied")
	}
}

func TestWebhookBearerOnlyRejectsBadHMAC(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	ctx := context.Background()
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer"],"rate_limit_per_min":60}`)

	body := `{}`
	ts := time.Now().UTC().Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.RemoteAddr = "1.1.1.1:1"
	req.Header.Set("X-AIE-Timestamp", ts)
	req.Header.Set("X-AIE-Signature", "00")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req, []byte(body)); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("Bearer-only 下假 HMAC 不能过, got %v", err)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req2.RemoteAddr = "1.1.1.1:1"
	req2.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req2, []byte(body)); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookEitherAuthModeAccepted(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	ctx := context.Background()
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["hmac","bearer"],"rate_limit_per_min":60}`)

	body := `{"x":1}`
	ts := time.Now().UTC().Format(time.RFC3339)
	reqH := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	reqH.RemoteAddr = "1.1.1.1:1"
	reqH.Header.Set("X-AIE-Timestamp", ts)
	reqH.Header.Set("X-AIE-Signature", signBody(secrets["hmac_secret"], ts, body))
	reqH.Header.Set("X-Idempotency-Key", "hmac-only")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqH, []byte(body)); err != nil {
		t.Fatal("双模式 HMAC 应过", err)
	}

	reqB := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	reqB.RemoteAddr = "1.1.1.1:1"
	reqB.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	reqB.Header.Set("X-Idempotency-Key", "bearer-only")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqB, []byte(body)); err != nil {
		t.Fatal("双模式 Bearer 应过", err)
	}

	// 两者都错
	reqBad := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	reqBad.RemoteAddr = "1.1.1.1:1"
	reqBad.Header.Set("X-AIE-Timestamp", ts)
	reqBad.Header.Set("X-AIE-Signature", "bad")
	reqBad.Header.Set("Authorization", "Bearer nope")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqBad, []byte(body)); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("双错应拒绝, got %v", err)
	}
}

func TestWebhookRateLimit(t *testing.T) {
	svc, _, audit := newTestSvc(t)
	ctx := context.Background()
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer"],"rate_limit_per_min":2}`)

	fire := func(idem string) error {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		req.RemoteAddr = "8.8.8.8:1"
		req.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
		req.Header.Set("X-Idempotency-Key", idem)
		_, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req, []byte("{}"))
		return err
	}
	if err := fire("r1"); err != nil {
		t.Fatal(err)
	}
	if err := fire("r2"); err != nil {
		t.Fatal(err)
	}
	if err := fire("r3"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("第 3 次应限流, got %v", err)
	}
	found := false
	for _, act := range audit.actions {
		if act == "automation.webhook_denied" {
			found = true
		}
	}
	if !found {
		t.Fatal("限流应审计 denied")
	}
}

func TestWebhookRotateSecretsInvalidateOld(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	ctx := context.Background()
	a, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer","hmac"],"rate_limit_per_min":60}`)
	oldBearer := secrets["bearer_token"]
	oldHMAC := secrets["hmac_secret"]

	neu, err := svc.RotateWebhookSecrets(ctx, a.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if neu["bearer_token"] == "" || neu["bearer_token"] == oldBearer {
		t.Fatal("轮换后应换新 bearer")
	}
	if neu["hmac_secret"] == "" || neu["hmac_secret"] == oldHMAC {
		t.Fatal("轮换后应换新 hmac")
	}

	reqOld := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	reqOld.RemoteAddr = "1.1.1.1:1"
	reqOld.Header.Set("Authorization", "Bearer "+oldBearer)
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqOld, []byte("{}")); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("旧 bearer 应失效, got %v", err)
	}

	reqNew := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	reqNew.RemoteAddr = "1.1.1.1:1"
	reqNew.Header.Set("Authorization", "Bearer "+neu["bearer_token"])
	reqNew.Header.Set("X-Idempotency-Key", "after-rotate")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqNew, []byte("{}")); err != nil {
		t.Fatal("新 bearer 应通过", err)
	}
}

func TestWebhookCIDRAllowlist(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	ctx := context.Background()
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer"],"ip_allowlist":["10.10.0.0/16"],"rate_limit_per_min":60}`)

	reqDeny := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	reqDeny.RemoteAddr = "11.0.0.1:1"
	reqDeny.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqDeny, []byte("{}")); !errors.Is(err, ErrForbiddenWebhook) {
		t.Fatalf("CIDR 外应拒绝, got %v", err)
	}

	reqOK := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	reqOK.RemoteAddr = "10.10.5.9:22"
	reqOK.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	reqOK.Header.Set("X-Idempotency-Key", "cidr-ok")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, reqOK, []byte("{}")); err != nil {
		t.Fatal(err)
	}
}

func TestWebhookXFFUsedForAllowlist(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	ctx := context.Background()
	_, secrets, cfg := createWebhook(t, svc, `{"auth_modes":["bearer"],"ip_allowlist":["203.0.113.50"],"rate_limit_per_min":60}`)

	// RemoteAddr 在白名单外，但 XFF 命中
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.RemoteAddr = "10.0.0.1:1"
	req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.0.1")
	req.Header.Set("Authorization", "Bearer "+secrets["bearer_token"])
	req.Header.Set("X-Idempotency-Key", "xff")
	if _, _, err := svc.HandleWebhook(ctx, cfg.PathToken, req, []byte("{}")); err != nil {
		t.Fatal("应按 XFF 首 IP 校验白名单", err)
	}
}

func TestWebhookCreateDoesNotExposeSecretsInStoredConfig(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	a, secrets, _ := createWebhook(t, svc, `{"auth_modes":["hmac","bearer"],"rate_limit_per_min":60}`)
	raw := string(a.TriggerConfig)
	if strings.Contains(raw, secrets["hmac_secret"]) || strings.Contains(raw, secrets["bearer_token"]) {
		t.Fatal("trigger_config 不得落库明文密钥")
	}
	got, err := svc.Get(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw2 := string(got.TriggerConfig)
	if strings.Contains(raw2, secrets["hmac_secret"]) || strings.Contains(raw2, secrets["bearer_token"]) {
		t.Fatal("Get 返回不得含明文密钥")
	}
}

func TestCalendarTerminalStatusesStopChain(t *testing.T) {
	for _, st := range []string{"CANCELLED", "TIMEOUT"} {
		t.Run(st, func(t *testing.T) {
			svc, jobs, _ := newTestSvc(t)
			ctx := context.Background()
			a, _, err := svc.Create(ctx, CreateInput{
				Name: "cal", TriggerType: TriggerCalendar, NotifyChatID: "oc", Timezone: "UTC",
			}, "admin")
			if err != nil {
				t.Fatal(err)
			}
			en := true
			items, err := svc.ReplaceCalendarItems(ctx, a.ID, "2026-06-01", []CalendarItemInput{
				{EmployeeID: "E1", Prompt: "a", Enabled: &en},
				{EmployeeID: "E2", Prompt: "b", Enabled: &en},
			}, "admin")
			if err != nil {
				t.Fatal(err)
			}
			svc.Tick(ctx, time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC))
			runs, _ := svc.ListRuns(ctx, a.ID, 10)
			if len(runs) != 1 {
				t.Fatalf("got %d", len(runs))
			}
			j := jobs.byID[runs[0].JobID]
			j.Status = st
			svc.OnJobTerminal(ctx, j)
			runs2, _ := svc.ListRuns(ctx, a.ID, 10)
			if len(runs2) != 1 {
				t.Fatalf("%s 后不应推进, got %d", st, len(runs2))
			}
			_ = items
		})
	}
}

func TestCronDisabledSkipped(t *testing.T) {
	svc, _, _ := newTestSvc(t)
	ctx := context.Background()
	cfg, _ := json.Marshal(CronConfig{Expr: "* * * * *"})
	en := false
	a, _, err := svc.Create(ctx, CreateInput{
		Name: "off", TriggerType: TriggerCron, Enabled: &en,
		EmployeeID: "E1", Prompt: "p", Timezone: "UTC", TriggerConfig: cfg,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	svc.Tick(ctx, time.Date(2026, 1, 1, 0, 0, 5, 0, time.UTC))
	runs, _ := svc.ListRuns(ctx, a.ID, 10)
	if len(runs) != 0 {
		t.Fatal("禁用 cron 不应触发")
	}
}
