package api_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/config"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookProxyReplayAndSizeBoundaries(t *testing.T) {
	h, token, _ := setupAutomationAPI(t)
	code, created := doJSON(t, h, "POST", "/api/automations", token, map[string]any{
		"name": "secure-hook", "trigger_type": "webhook", "employee_id": "E1", "prompt": "test",
		"trigger_config": map[string]any{"auth_modes": []string{"hmac"}, "ip_allowlist": []string{"203.0.113.50"}},
	})
	if code != 201 {
		t.Fatal(code, created)
	}
	cfg := created["item"].(map[string]any)["trigger_config"].(map[string]any)
	key := created["secrets"].(map[string]any)["hmac_secret"].(string)
	_, network, _ := net.ParseCIDR("172.30.20.10/32")
	protected := api.SecureHTTP(h, &config.Config{TrustedProxies: []*net.IPNet{network}})
	ts := time.Now().UTC().Format(time.RFC3339)
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(ts + ".{}"))
	sig := hex.EncodeToString(mac.Sum(nil))
	for _, tc := range []struct {
		name, remote, xff, idem, body string
		want                          int
	}{
		{"spoofed direct request", "203.0.113.51:123", "203.0.113.50", "one", "{}", 401},
		{"spoofed first proxy hop", "172.30.20.10:123", "203.0.113.50, 203.0.113.51", "one", "{}", 401},
		{"trusted proxy with actual client", "172.30.20.10:123", "203.0.113.50", "one", "{}", 202},
		{"signed replay with changed unsigned key", "172.30.20.10:123", "203.0.113.50", "different", "{}", 409},
		{"oversized payload", "172.30.20.10:123", "203.0.113.50", "huge", strings.Repeat("x", (1<<20)+1), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/integrations/automation/hooks/"+cfg["path_token"].(string), strings.NewReader(tc.body))
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", tc.xff)
			r.Header.Set("X-AIE-Timestamp", ts)
			r.Header.Set("X-AIE-Signature", sig)
			r.Header.Set("X-Idempotency-Key", tc.idem)
			rr := httptest.NewRecorder()
			protected.ServeHTTP(rr, r)
			if rr.Code != tc.want {
				t.Fatalf("want %d got %d: %s", tc.want, rr.Code, rr.Body.String())
			}
		})
	}
}
