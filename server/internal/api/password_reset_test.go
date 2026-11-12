package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
)

func TestPasswordResetSelfAndAdministrator(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("root", "root-password", "Root"); err != nil {
		t.Fatal(err)
	}
	root, _ := users.FindByUsername(ctx, "root")
	if err := users.SetRoles(ctx, root.ID, []string{"SUPER_ADMIN"}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []struct{ id, role string }{{"alice", "VIEWER"}, {"bob", "USER"}, {"manager", "ADMIN"}} {
		hash, _ := auth.HashPassword("original-password")
		if err := users.Create(ctx, &auth.User{ID: account.id, Username: account.id, PasswordHash: hash, Roles: []string{account.role}, Status: auth.StatusActive}); err != nil {
			t.Fatal(err)
		}
	}
	service := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	h := api.NewRouter(api.Deps{Auth: service})
	login := func(name, password string) string {
		t.Helper()
		session, err := service.Login(ctx, name, password, "")
		if err != nil {
			t.Fatal(err)
		}
		return session.Token
	}
	alice := login("alice", "original-password")
	otherSession := login("alice", "original-password")
	manager := login("manager", "original-password")
	code, body := doJSON(t, h, http.MethodGet, "/api/users/alice", alice, nil)
	if code != http.StatusOK {
		t.Fatal(code, body)
	}
	code, _ = doJSON(t, h, http.MethodPatch, "/api/users/bob", alice, map[string]string{"password": "hijacked-password"})
	if code != http.StatusForbidden {
		t.Fatalf("ordinary user reset another account: %d", code)
	}
	code, _ = doJSON(t, h, http.MethodPatch, "/api/users/alice", alice, map[string]string{"password": "short"})
	if code != http.StatusBadRequest {
		t.Fatalf("weak password: %d", code)
	}
	code, body = doJSON(t, h, http.MethodPatch, "/api/users/alice", alice, map[string]string{"password": "new-alice-password"})
	if code != http.StatusOK {
		t.Fatal(code, body)
	}
	for _, token := range []string{alice, otherSession} {
		if _, err := service.Authenticate(ctx, token); err == nil {
			t.Fatal("old session remains valid after password reset")
		}
	}
	if _, err := service.Login(ctx, "alice", "original-password", ""); err == nil {
		t.Fatal("old password remains valid")
	}
	_ = login("alice", "new-alice-password")
	code, body = doJSON(t, h, http.MethodPatch, "/api/users/alice", manager, map[string]string{"password": "manager-reset-password"})
	if code != http.StatusOK {
		t.Fatal(code, body)
	}
	_ = login("alice", "manager-reset-password")
	code, _ = doJSON(t, h, http.MethodPatch, "/api/users/"+root.ID, manager, map[string]string{"password": "hijacked-password"})
	if code != http.StatusForbidden {
		t.Fatalf("manager reset super administrator: %d", code)
	}
}
