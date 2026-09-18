package auth_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
)

func TestLoginAndRBAC(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	if err := users.SeedAdmin("admin", "secret", "Admin"); err != nil {
		t.Fatal(err)
	}
	svc := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())

	if _, err := svc.Login(ctx, "admin", "wrong", "127.0.0.1"); err == nil {
		t.Fatal("错误密码应失败")
	}
	sess, err := svc.Login(ctx, "admin", "secret", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Authenticate(ctx, sess.Token)
	if err != nil || got.Username != "admin" {
		t.Fatalf("会话无效: %v %#v", err, got)
	}
	if err := svc.Authorize(ctx, sess, "employee.read"); err != nil {
		t.Fatal(err)
	}
	// VIEWER 角色用户
	hash, _ := auth.HashPassword("v")
	_ = users.Update(ctx, &auth.User{ID: "u2", Username: "viewer", PasswordHash: hash, Roles: []string{"VIEWER"}})
	vs, err := svc.Login(ctx, "viewer", "v", "10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Authorize(ctx, vs, "employee.write"); err != auth.ErrForbidden {
		t.Fatalf("VIEWER 不应有 write: %v", err)
	}
}
