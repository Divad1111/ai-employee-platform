package auth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
)

func TestCustomManagementRolesRequireMFA(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	s := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	s.EnforceAdminMFA = true
	for _, code := range []string{"infra.manage", "secret.write", "role.update", "backup.restore", "workflow.write", "*"} {
		if err := users.CreateRole(ctx, strings.ToUpper(code), "", []auth.PermissionGrant{{Code: code, Scope: "ALL"}}); err != nil {
			t.Fatal(err)
		}
		if !s.RequiresMFA(ctx, []string{strings.ToUpper(code)}) {
			t.Errorf("%s bypassed MFA", code)
		}
	}
	_ = users.CreateRole(ctx, "reader", "", []auth.PermissionGrant{{Code: "employee.read", Scope: "OWN"}})
	if s.RequiresMFA(ctx, []string{"reader"}) {
		t.Fatal("ordinary reader unnecessarily requires MFA")
	}
	_ = users.CreateRole(ctx, "CUSTOM_ADMIN", "", []auth.PermissionGrant{{Code: "infra.manage", Scope: "ALL"}})
	hash, _ := auth.HashPassword("custom-password-123")
	_ = users.Create(ctx, &auth.User{ID: "custom", Username: "custom", PasswordHash: hash, Status: auth.StatusActive, Roles: []string{"CUSTOM_ADMIN"}})
	sess, err := s.Login(ctx, "custom", "custom-password-123", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.Authenticate(ctx, sess.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !current.MFARequired || s.AuthorizeAll(ctx, current, "infra.manage") == nil {
		t.Fatal("custom administrator bypassed MFA through a live session")
	}
	if err := s.ConfirmMFA(ctx, sess.Token); err != nil {
		t.Fatal(err)
	}
	current, err = s.Authenticate(ctx, sess.Token)
	if err != nil || s.AuthorizeAll(ctx, current, "infra.manage") != nil {
		t.Fatal("verified custom administrator denied")
	}
}

func TestReauthenticationIsRateLimitedWithoutInvalidatingSession(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "correct", "Admin")
	s := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	sess, _ := s.Login(ctx, "admin", "correct", "127.0.0.1")
	for i := 0; i < 20; i++ {
		if !errors.Is(s.VerifyPassword(ctx, "admin", "wrong"), auth.ErrInvalidCredentials) {
			t.Fatal(i)
		}
	}
	if !errors.Is(s.VerifyPassword(ctx, "admin", "correct"), auth.ErrReauthLimited) {
		t.Fatal("reauth rate limit bypassed")
	}
	if _, err := s.Authenticate(ctx, sess.Token); err != nil {
		t.Fatal("rate limiting destroyed login", err)
	}
}
