package api_test

import (
	"bytes"
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

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/automation"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/secret"
)

func setupAutomationAPI(t *testing.T) (http.Handler, string, *automation.Service) {
	t.Helper()
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Administrator"); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	vault, _ := secret.NewMemoryVault()
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, nil)
	autoSvc := automation.New(automation.NewMemoryStore(), jobSvc, nil, vault, auditor)
	h := api.NewRouter(api.Deps{
		Auth: authSvc, Automation: autoSvc, Audit: auditor, Jobs: jobSvc, Secrets: vault,
	})
	return h, login(t, h), autoSvc
}

func TestAutomationWebhookHTTPStatuses(t *testing.T) {
	h, tok, _ := setupAutomationAPI(t)

	code, created := doJSON(t, h, "POST", "/api/automations", tok, map[string]any{
		"name": "hook", "trigger_type": "webhook", "employee_id": "E1", "prompt": "p",
		"trigger_config": map[string]any{
			"auth_modes": []string{"hmac", "bearer"}, "ip_allowlist": []string{"127.0.0.1"}, "rate_limit_per_min": 5,
		},
	})
	if code != http.StatusCreated {
		t.Fatalf("create %d %#v", code, created)
	}
	item := created["item"].(map[string]any)
	secrets := created["secrets"].(map[string]any)
	cfg := item["trigger_config"].(map[string]any)
	pathToken := cfg["path_token"].(string)
	bearer := secrets["bearer_token"].(string)
	hmacSec := secrets["hmac_secret"].(string)

	// 列表/详情不得回说明文密钥
	code, listed := doJSON(t, h, "GET", "/api/automations?type=webhook", tok, nil)
	if code != 200 {
		t.Fatal(code)
	}
	blob, _ := json.Marshal(listed)
	if strings.Contains(string(blob), bearer) || strings.Contains(string(blob), hmacSec) {
		t.Fatal("列表响应不得含明文密钥")
	}

	postHook := func(path string, headers map[string]string, body string, remote string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.RemoteAddr = remote
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	url := "/api/integrations/automation/hooks/" + pathToken

	// 404 未知 token（不同 path，不占本 hook 限流）
	if c := postHook("/api/integrations/automation/hooks/nope", map[string]string{
		"Authorization": "Bearer " + bearer,
	}, "{}", "127.0.0.1:1"); c != http.StatusNotFound {
		t.Fatalf("未知 hook 应 404, got %d", c)
	}

	// 401 IP（限流前拦截，不占额度）
	if c := postHook(url, map[string]string{
		"Authorization": "Bearer " + bearer,
	}, "{}", "8.8.8.8:1"); c != http.StatusUnauthorized {
		t.Fatalf("非白名单 IP 应 401, got %d", c)
	}

	// 202 合法 Bearer
	if c := postHook(url, map[string]string{
		"Authorization":     "Bearer " + bearer,
		"X-Idempotency-Key": "http-1",
	}, "{}", "127.0.0.1:1"); c != http.StatusAccepted {
		t.Fatalf("合法应 202, got %d", c)
	}

	// 202 合法 HMAC
	body := `{"n":2}`
	ts := time.Now().UTC().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(hmacSec))
	_, _ = mac.Write([]byte(ts + "." + body))
	sig := hex.EncodeToString(mac.Sum(nil))
	if c := postHook(url, map[string]string{
		"X-AIE-Timestamp":   ts,
		"X-AIE-Signature":   sig,
		"X-Idempotency-Key": "http-2",
	}, body, "127.0.0.1:1"); c != http.StatusAccepted {
		t.Fatalf("HMAC 应 202, got %d", c)
	}

	// 401 错签（通过 IP 后仍计限流，再拒绝鉴权）
	if c := postHook(url, map[string]string{
		"X-AIE-Timestamp": ts,
		"X-AIE-Signature": "00",
	}, "{}", "127.0.0.1:1"); c != http.StatusUnauthorized {
		t.Fatalf("错签应 401, got %d", c)
	}

	// 再打满额度：已用 3 次（2 成功 + 1 错签），rate=5 → 还可 2 次成功，第 6 次 429
	for i := 4; i <= 5; i++ {
		if c := postHook(url, map[string]string{
			"Authorization":     "Bearer " + bearer,
			"X-Idempotency-Key": "http-" + string(rune('0'+i)),
		}, "{}", "127.0.0.1:1"); c != http.StatusAccepted {
			t.Fatalf("第 %d 次应 202, got %d", i, c)
		}
	}
	if c := postHook(url, map[string]string{
		"Authorization":     "Bearer " + bearer,
		"X-Idempotency-Key": "http-overflow",
	}, "{}", "127.0.0.1:1"); c != http.StatusTooManyRequests {
		t.Fatalf("超限应 429, got %d", c)
	}
}

func TestAutomationViewerCannotWrite(t *testing.T) {
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin123", "Administrator")
	hash, _ := auth.HashPassword("view-pass")
	_ = users.Create(context.Background(), &auth.User{
		ID: "u-v", Username: "viewer1", PasswordHash: hash, DisplayName: "V", Roles: []string{"VIEWER"},
	})
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	vault, _ := secret.NewMemoryVault()
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, nil)
	autoSvc := automation.New(automation.NewMemoryStore(), jobSvc, nil, vault, auditor)
	h := api.NewRouter(api.Deps{Auth: authSvc, Automation: autoSvc, Audit: auditor, Jobs: jobSvc, Secrets: vault})

	loginBody, _ := json.Marshal(map[string]string{"username": "viewer1", "password": "view-pass"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &loginResp)

	code, _ := doJSON(t, h, "GET", "/api/automations", loginResp.Token, nil)
	if code != 200 {
		t.Fatalf("VIEWER 应可读, got %d", code)
	}
	code, _ = doJSON(t, h, "POST", "/api/automations", loginResp.Token, map[string]any{
		"name": "x", "trigger_type": "cron", "employee_id": "E", "prompt": "p",
		"trigger_config": map[string]string{"preset": "daily"},
	})
	if code != http.StatusForbidden {
		t.Fatalf("VIEWER 写应 403, got %d", code)
	}
}

func TestAutomationWebhookUnauthenticatedNoSessionNeeded(t *testing.T) {
	h, tok, _ := setupAutomationAPI(t)
	code, created := doJSON(t, h, "POST", "/api/automations", tok, map[string]any{
		"name": "pub", "trigger_type": "webhook", "employee_id": "E1", "prompt": "p",
		"trigger_config": map[string]any{"auth_modes": []string{"bearer"}, "rate_limit_per_min": 10},
	})
	if code != 201 {
		t.Fatal(code, created)
	}
	secrets := created["secrets"].(map[string]any)
	cfg := created["item"].(map[string]any)["trigger_config"].(map[string]any)
	path := cfg["path_token"].(string)

	// 故意不带 Admin Session，只带 Bearer
	req := httptest.NewRequest(http.MethodPost, "/api/integrations/automation/hooks/"+path, strings.NewReader("{}"))
	req.RemoteAddr = "9.9.9.9:1"
	req.Header.Set("Authorization", "Bearer "+secrets["bearer_token"].(string))
	req.Header.Set("X-Idempotency-Key", "no-sess")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("入站 webhook 不应要求登录, got %d %s", rr.Code, rr.Body.String())
	}
}
