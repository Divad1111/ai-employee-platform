package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/automation"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/secret"
)

func TestAutomationWriteRequiresAdmin(t *testing.T) {
	auditor := audit.NewMemory()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "admin123", "Administrator"); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("op-pass")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Create(context.Background(), &auth.User{
		ID: "u-op", Username: "operator1", PasswordHash: hash, DisplayName: "Op", Roles: []string{"OPERATOR"},
	}); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), auditor)
	vault, _ := secret.NewMemoryVault()
	jobSvc := job.NewService(job.NewMemoryStore(), auditor, nil)
	autoSvc := automation.New(automation.NewMemoryStore(), jobSvc, nil, vault, auditor)

	h := api.NewRouter(api.Deps{
		Auth: authSvc, Automation: autoSvc, Audit: auditor, Jobs: jobSvc, Secrets: vault,
	})

	loginBody, _ := json.Marshal(map[string]string{"username": "operator1", "password": "op-pass"})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(loginBody)))
	if w.Code != 200 {
		t.Fatalf("operator 登录失败: %d %s", w.Code, w.Body.String())
	}
	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &loginResp)

	createBody, _ := json.Marshal(map[string]any{
		"name": "x", "trigger_type": "cron", "employee_id": "E1", "prompt": "p",
		"trigger_config": map[string]string{"preset": "daily"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/automations", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+loginResp.Token)
	req.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, req)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("OPERATOR 写自动化应 403, got %d %s", w2.Code, w2.Body.String())
	}

	// ADMIN 可读可写
	adminTok := login(t, h)
	code, _ := doJSON(t, h, "POST", "/api/automations", adminTok, map[string]any{
		"name": "cron-ok", "trigger_type": "cron", "employee_id": "E1", "prompt": "p",
		"trigger_config": map[string]string{"preset": "daily"},
	})
	if code != http.StatusCreated {
		t.Fatalf("ADMIN 创建应 201, got %d", code)
	}
}
