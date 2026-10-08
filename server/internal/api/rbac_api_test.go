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
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/quota"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/ai-employee-platform/server/internal/wsmember"
)

func TestRBAC_UsersAndScope(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin12345", "Admin")
	sessions := auth.NewMemorySessionStore()
	auditor := audit.NewMemory()
	authSvc := auth.NewService(users, sessions, auditor)
	empStore := employee.NewMemoryStore()
	empSvc := employee.NewService(empStore, auditor, nil)
	wsMembers := wsmember.NewMemoryStore()
	wsMeta := workstation.NewMemoryMeta()
	wsSvc := workstation.NewService(nil, nil, wsMeta)
	quotaSvc := quota.NewService(quota.NewMemoryStore())

	h := api.NewRouter(api.Deps{
		Auth:         authSvc,
		Employees:    empSvc,
		Workstations: wsSvc,
		Audit:        auditor,
		WSMembers:    wsMembers,
		Quota:        quotaSvc,
	})

	login := func(user, pass string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("login %s: %d %s", user, rr.Code, rr.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		tok, _ := resp["token"].(string)
		if tok == "" {
			t.Fatal("empty token")
		}
		return tok
	}

	adminTok := login("admin", "admin12345")

	// /me 含 permissions+scope
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("me: %d", rr.Code)
	}
	var me map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &me)
	perms, _ := me["permissions"].([]any)
	if len(perms) == 0 {
		t.Fatal("expected permissions with scope")
	}

	// 创建 VIEWER
	createBody, _ := json.Marshal(map[string]any{
		"username": "alice", "password": "alice12345", "display_name": "Alice", "roles": []string{"VIEWER"},
	})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create user: %d %s", rr.Code, rr.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	aliceID, _ := created["id"].(string)

	aliceTok := login("alice", "alice12345")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Authorization", "Bearer "+aliceTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer users list want 403 got %d", rr.Code)
	}

	// 禁用后无法登录
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/users/"+aliceID+"/disable", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("disable: %d %s", rr.Code, rr.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"username": "alice", "password": "alice12345"})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("disabled login want 403 got %d body=%s", rr.Code, rr.Body.String())
	}

	// 启用 + Workstation 成员
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/users/"+aliceID+"/enable", nil)
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)

	_ = wsMeta.Upsert(ctx, "WS-1", "Node1")
	adminUser, _ := users.FindByUsername(ctx, "admin")
	_ = wsMembers.Upsert(ctx, &wsmember.Membership{
		WorkstationID: "WS-1", UserID: adminUser.ID, Role: wsmember.RoleOwner, Status: wsmember.StatusActive,
	})

	empBody, _ := json.Marshal(map[string]any{"name": "E1", "workstation_id": "WS-1"})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/employees", bytes.NewReader(empBody))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create emp: %d %s", rr.Code, rr.Body.String())
	}

	aliceTok = login("alice", "alice12345")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/employees", nil)
	req.Header.Set("Authorization", "Bearer "+aliceTok)
	h.ServeHTTP(rr, req)
	var empList map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &empList)
	items, _ := empList["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("viewer should not see admin employees, got %d", len(items))
	}

	// OPERATOR 绑未授权站 → WORKSTATION_ACCESS_DENIED
	opBody, _ := json.Marshal(map[string]any{
		"username": "bob", "password": "bob123456", "roles": []string{"OPERATOR"},
	})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(opBody))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create bob: %d %s", rr.Code, rr.Body.String())
	}
	bobTok := login("bob", "bob123456")
	empBody, _ = json.Marshal(map[string]any{"name": "E2", "workstation_id": "WS-NOPE"})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/employees", bytes.NewReader(empBody))
	req.Header.Set("Authorization", "Bearer "+bobTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden || !bytes.Contains(rr.Body.Bytes(), []byte("WORKSTATION_ACCESS_DENIED")) {
		t.Fatalf("want WORKSTATION_ACCESS_DENIED 403 got %d %s", rr.Code, rr.Body.String())
	}

	if err := quotaSvc.Check(ctx, quota.TypeUser, adminUser.ID, 20, 1); err != nil {
		t.Fatalf("no policy should allow: %v", err)
	}
	_ = quotaSvc.Store().UpsertPolicy(ctx, &quota.Policy{
		ResourceType: quota.TypeUser, ResourceID: adminUser.ID, PeriodType: quota.PeriodMonthly,
		TokenLimit: 10, Enabled: true,
	})
	if err := quotaSvc.Check(ctx, quota.TypeUser, adminUser.ID, 20, 1); err != quota.ErrExceeded {
		t.Fatalf("want quota_exceeded got %v", err)
	}
}

func TestSuperAdminChangesOtherUserRole(t *testing.T) {
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "admin12345", "Admin")
	hash, err := auth.HashPassword("super12345")
	if err != nil {
		t.Fatal(err)
	}
	super := &auth.User{Username: "root", PasswordHash: hash, DisplayName: "Root", Status: auth.StatusActive, Roles: []string{"SUPER_ADMIN"}}
	if err := users.Create(context.Background(), super); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	h := api.NewRouter(api.Deps{Auth: authSvc, Audit: audit.NewMemory()})

	login := func(user, pass string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("login %s: %d %s", user, rr.Code, rr.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rr.Body.Bytes(), &resp)
		return resp["token"].(string)
	}
	adminTok := login("admin", "admin12345")
	superTok := login("root", "super12345")

	createBody, _ := json.Marshal(map[string]any{
		"username": "alice", "password": "alice12345", "roles": []string{"USER"},
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/users", bytes.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer "+adminTok)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	aliceID := created["id"].(string)

	patch := func(tok, id string, roles []string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]any{"roles": roles})
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/users/"+id, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := patch(adminTok, aliceID, []string{"OPERATOR"}); rr.Code != http.StatusForbidden {
		t.Fatalf("普通管理员不能改角色: %d %s", rr.Code, rr.Body.String())
	}
	if rr := patch(superTok, aliceID, []string{"OPERATOR"}); rr.Code != http.StatusOK {
		t.Fatalf("超级管理员应能改其他用户角色: %d %s", rr.Code, rr.Body.String())
	}
	if rr := patch(superTok, super.ID, []string{"ADMIN"}); rr.Code != http.StatusForbidden {
		t.Fatalf("不能改超级管理员角色: %d %s", rr.Code, rr.Body.String())
	}
}
