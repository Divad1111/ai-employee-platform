package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
)

func TestSetupAPI_Workflow(t *testing.T) {
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore() // 未初始化
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)

	handler := api.NewRouter(api.Deps{
		Auth:  authSvc,
		Audit: auditor,
	})

	// 1. 测试未初始化时状态
	{
		req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("setup status expected 200, got %d", rec.Code)
		}
		var status map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &status)
		if status["initialized"] != false || status["needs_setup"] != true {
			t.Fatalf("expected uninitialized status, got: %#v", status)
		}
	}

	// 2. 测试弱密码被拒绝 (400)
	{
		body, _ := json.Marshal(map[string]string{
			"username": "superadmin",
			"password": "123", // 太短
		})
		req := httptest.NewRequest(http.MethodPost, "/api/setup/init", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("weak password expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3. 测试首次初始化成功 (201 Created)
	var createdToken string
	{
		body, _ := json.Marshal(map[string]string{
			"username":     "rootadmin",
			"password":     "SuperSecurePass123!",
			"display_name": "系统超级管理员",
			"system_name":  "AI Employee Platform",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/setup/init", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("setup init expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		token, ok := resp["token"].(string)
		if !ok || token == "" {
			t.Fatalf("expected valid session token in setup init response: %#v", resp)
		}
		createdToken = token
	}

	// 4. 再次获取状态，应变为已初始化
	{
		req := httptest.NewRequest(http.MethodGet, "/api/setup/status", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("setup status expected 200, got %d", rec.Code)
		}
		var status map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &status)
		if status["initialized"] != true || status["needs_setup"] != false {
			t.Fatalf("expected initialized status, got: %#v", status)
		}
	}

	// 5. 再次尝试调用 init，应被拦截 (409 Conflict)
	{
		body, _ := json.Marshal(map[string]string{
			"username": "secondadmin",
			"password": "SuperSecurePass123!",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/setup/init", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusConflict {
			t.Fatalf("duplicate setup expected 409 Conflict, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 6. 使用创建的管理员 token 请求 /api/auth/me
	{
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+createdToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("auth/me expected 200 with init session token, got %d: %s", rec.Code, rec.Body.String())
		}
		var me map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &me)
		if me["username"] != "rootadmin" {
			t.Fatalf("expected rootadmin, got %#v", me)
		}
	}
}
