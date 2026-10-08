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
	_ = users.Create(ctx, &auth.User{ID: "u2", Username: "viewer", PasswordHash: hash, Status: auth.StatusActive, Roles: []string{"VIEWER"}})
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

func TestDefaultRoleUSER(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()

	// 1. 验证 USER 是内置角色，不可删除
	if !auth.IsBuiltInRole("USER") {
		t.Fatal("USER 应当是内置角色")
	}
	if err := users.DeleteRole(ctx, "USER"); err == nil {
		t.Fatal("删除内置角色 USER 应当失败")
	}

	// 2. 验证 ListRoles 包含 USER，且所有权限的 Scope 均为 OWN
	roles, err := users.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles 失败: %v", err)
	}
	var userRole *auth.RoleInfo
	for i := range roles {
		if roles[i].Name == "USER" {
			userRole = &roles[i]
			break
		}
	}
	if userRole == nil {
		t.Fatal("ListRoles 中未找到 USER 角色")
	}
	if len(userRole.Grants) == 0 {
		t.Fatal("USER 角色的权限列表不应为空")
	}
	for _, g := range userRole.Grants {
		if g.Scope != "OWN" {
			t.Fatalf("USER 角色的权限 %s 的 Scope 期望为 OWN，实际为 %s", g.Code, g.Scope)
		}
	}

	// 3. 验证新建用户不填角色时默认赋 USER 角色
	hash, _ := auth.HashPassword("pwd123456")
	u := &auth.User{
		Username:     "regular_user",
		PasswordHash: hash,
		DisplayName:  "Regular",
		Status:       auth.StatusActive,
	}
	if err := users.Create(ctx, u); err != nil {
		t.Fatalf("Create 用户失败: %v", err)
	}
	if len(u.Roles) != 1 || u.Roles[0] != "USER" {
		t.Fatalf("用户默认角色期望为 USER，实际为 %v", u.Roles)
	}
}


