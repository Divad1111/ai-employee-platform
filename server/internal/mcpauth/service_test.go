package mcpauth_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/mcpauth"
)

func TestTokenIssueAndValidate(t *testing.T) {
	ctx := context.Background()
	store := mcpauth.NewMemoryStore()
	svc := mcpauth.NewService(store)

	// 1. 测试非法 subject_type
	if _, err := svc.Issue(ctx, "INVALID", "id-1", "", "test", "admin", 0); err == nil {
		t.Fatal("expected error for invalid subject type")
	}

	// 2. 测试 Employee 默认 scope (READ)
	empRes, err := svc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-1", "", "emp-token", "admin", 2*time.Hour)
	if err != nil {
		t.Fatalf("issue employee token failed: %v", err)
	}
	if !strings.HasPrefix(empRes.Secret, mcpauth.Prefix) {
		t.Fatalf("secret should start with %s, got %s", mcpauth.Prefix, empRes.Secret)
	}
	if empRes.Token.Scope != mcpauth.ScopeRead {
		t.Fatalf("expected scope %s, got %s", mcpauth.ScopeRead, empRes.Token.Scope)
	}
	if empRes.Token.ExpiresAt == nil {
		t.Fatal("expected non-nil expires_at")
	}

	// 3. 测试 User 默认 scope (ADMIN)
	userRes, err := svc.Issue(ctx, mcpauth.SubjectUser, "USER-1", "", "user-token", "admin", 0)
	if err != nil {
		t.Fatalf("issue user token failed: %v", err)
	}
	if userRes.Token.Scope != mcpauth.ScopeAdmin {
		t.Fatalf("expected scope %s, got %s", mcpauth.ScopeAdmin, userRes.Token.Scope)
	}
	if userRes.Token.ExpiresAt != nil {
		t.Fatal("expected nil expires_at for non-expiring token")
	}

	// 4. 测试 Validate 成功与 LastUsedAt 刷新
	validated, err := svc.Validate(ctx, empRes.Secret)
	if err != nil {
		t.Fatalf("validate failed: %v", err)
	}
	if validated.ID != empRes.Token.ID {
		t.Fatalf("expected id %s, got %s", empRes.Token.ID, validated.ID)
	}
	if validated.LastUsedAt == nil {
		t.Fatal("expected LastUsedAt to be updated")
	}

	// 5. 测试 Validate 非法前缀与不存在 Token
	if _, err := svc.Validate(ctx, "invalid_token"); err != mcpauth.ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
	if _, err := svc.Validate(ctx, mcpauth.Prefix+"00000000000000000000000000000000"); err != mcpauth.ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken for unknown token, got %v", err)
	}
}

func TestTokenExpiration(t *testing.T) {
	ctx := context.Background()
	store := mcpauth.NewMemoryStore()
	svc := mcpauth.NewService(store)

	// 签发极短有效期的 Token
	res, err := svc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-EXP", mcpauth.ScopeRead, "exp", "admin", 2*time.Millisecond)
	if err != nil {
		t.Fatalf("issue failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	_, err = svc.Validate(ctx, res.Secret)
	if err != mcpauth.ErrExpired {
		t.Fatalf("expected ErrExpired, got %v", err)
	}
}

func TestTokenRevocation(t *testing.T) {
	ctx := context.Background()
	store := mcpauth.NewMemoryStore()
	svc := mcpauth.NewService(store)

	res, err := svc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-REV", mcpauth.ScopeRead, "rev", "admin", time.Hour)
	if err != nil {
		t.Fatalf("issue failed: %v", err)
	}

	// 吊销前有效
	if _, err := svc.Validate(ctx, res.Secret); err != nil {
		t.Fatalf("validate before revoke failed: %v", err)
	}

	// 吊销
	if err := svc.Revoke(ctx, res.Token.ID); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	// 吊销后验证返回 ErrRevoked
	if _, err := svc.Validate(ctx, res.Secret); err != mcpauth.ErrRevoked {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
}

func TestTokenDeleteAndList(t *testing.T) {
	ctx := context.Background()
	store := mcpauth.NewMemoryStore()
	svc := mcpauth.NewService(store)

	r1, _ := svc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-X", mcpauth.ScopeRead, "t1", "admin", 0)
	r2, _ := svc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-X", mcpauth.ScopeRead, "t2", "admin", 0)
	_, _ = svc.Issue(ctx, mcpauth.SubjectEmployee, "EMP-Y", mcpauth.ScopeRead, "t3", "admin", 0)

	list, err := svc.ListBySubject(ctx, mcpauth.SubjectEmployee, "EMP-X")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(list))
	}

	// 删除 r1
	if err := svc.Delete(ctx, r1.Token.ID); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	listAfter, err := svc.ListBySubject(ctx, mcpauth.SubjectEmployee, "EMP-X")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(listAfter) != 1 || listAfter[0].ID != r2.Token.ID {
		t.Fatalf("expected 1 token (r2), got %v", listAfter)
	}

	// 验证已删除的 Token validate 失败
	if _, err := svc.Validate(ctx, r1.Secret); err != mcpauth.ErrInvalidToken {
		t.Fatalf("expected ErrInvalidToken, got %v", err)
	}
}
