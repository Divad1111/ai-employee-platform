package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/config"
)

func TestBrowserCookieSessionAndCSRF(t *testing.T) {
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "test-password-123", "Admin")
	a := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	h := api.NewRouter(api.Deps{Auth: a, Config: &config.Config{Env: "production"}})
	req := httptest.NewRequest("POST", "https://platform.example/api/auth/login", strings.NewReader(`{"username":"admin","password":"test-password-123"}`))
	req.Header.Set("X-AIE-Browser", "1")
	req.Header.Set("Origin", "https://platform.example")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"token":""`) {
		t.Fatal(rr.Code, rr.Body.String())
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.MaxAge <= 0 {
		t.Fatal("unsafe cookie", cookie)
	}
	check := func(method, path, origin, header string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "https://platform.example"+path, nil)
		req.AddCookie(cookie)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if header != "" {
			req.Header.Set("X-AIE-Browser", header)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := check("GET", "/api/auth/me", "", ""); rr.Code != 200 {
		t.Fatal("cookie auth failed", rr.Code)
	}
	for _, tt := range []struct{ origin, header string }{{"https://evil.example", "1"}, {"null", "1"}, {"https://platform.example", ""}} {
		if rr := check("POST", "/api/auth/logout", tt.origin, tt.header); rr.Code != 403 {
			t.Fatal("CSRF accepted", rr.Code)
		}
	}
	rr = check("POST", "/api/auth/logout", "https://platform.example", "1")
	if rr.Code != 200 || rr.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if rr := check("GET", "/api/auth/me", "", ""); rr.Code != 401 {
		t.Fatal("logged-out session still works")
	}
}

func TestStepUpEndpointRateLimits(t *testing.T) {
	h, token := setupAPI(t)
	for i := 0; i < 20; i++ {
		code, _ := doJSON(t, h, "POST", "/api/auth/step-up", token, map[string]string{"password": "wrong"})
		if code != 403 {
			t.Fatal(i, code)
		}
	}
	if code, _ := doJSON(t, h, "POST", "/api/auth/step-up", token, map[string]string{"password": "wrong"}); code != 429 {
		t.Fatal("rate limit missing", code)
	}
	if code, _ := doJSON(t, h, "GET", "/api/auth/me", token, nil); code != 200 {
		t.Fatal("reauth failure destroyed session")
	}
}
