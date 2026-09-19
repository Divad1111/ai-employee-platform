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

func TestInitAdmin(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	svc := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())

	// 初始状态应未初始化
	init, err := svc.IsInitialized(ctx)
	if err != nil || init {
		t.Fatalf("初始状态应为未初始化，got init=%v, err=%v", init, err)
	}

	// 弱密码校验
	if _, err := svc.InitAdmin(ctx, "rootadmin", "123", "Root", "127.0.0.1"); err != auth.ErrWeakPassword {
		t.Fatalf("短密码应报错 ErrWeakPassword，got: %v", err)
	}

	// 短用户名校验
	if _, err := svc.InitAdmin(ctx, "ro", "Password123!", "Root", "127.0.0.1"); err != auth.ErrInvalidUsername {
		t.Fatalf("短用户名应报错 ErrInvalidUsername，got: %v", err)
	}

	// 首次成功创建管理员
	sess, err := svc.InitAdmin(ctx, "customadmin", "Password123!", "超级管理员", "127.0.0.1")
	if err != nil {
		t.Fatalf("首次初始化失败: %v", err)
	}
	if sess == nil || sess.Token == "" || sess.Username != "customadmin" {
		t.Fatalf("生成的会话不正确: %#v", sess)
	}

	// 状态应变为已初始化
	init, err = svc.IsInitialized(ctx)
	if err != nil || !init {
		t.Fatalf("初始化后应为 true，got init=%v, err=%v", init, err)
	}

	// 再次初始化应拒绝
	if _, err := svc.InitAdmin(ctx, "anotheradmin", "Password123!", "另一个", "127.0.0.1"); err != auth.ErrAlreadyInitialized {
		t.Fatalf("重复初始化应拒绝并报错 ErrAlreadyInitialized，got: %v", err)
	}

	// 能够使用新创建的密码登录
	loginSess, err := svc.Login(ctx, "customadmin", "Password123!", "127.0.0.1")
	if err != nil || loginSess == nil {
		t.Fatalf("新管理员应能正常登录: %v", err)
	}
}

