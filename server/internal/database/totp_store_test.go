package database_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/database"
	"github.com/ai-employee-platform/server/internal/secret"
	"github.com/ai-employee-platform/server/internal/totp"
)

func TestPostgresTOTPStore(t *testing.T) {
	connStr := os.Getenv("AIE_DATABASE_URL")
	if connStr == "" {
		connStr = "postgres://aie:aie@localhost:5432/aie?sslmode=disable"
	}
	db, err := database.Open(connStr)
	if err != nil {
		t.Skipf("Skipping Postgres TOTP test, cannot connect to %s: %v", connStr, err)
		return
	}
	defer db.Close()

	ctx := context.Background()
	store := db.NewTOTPStore()

	// 1. 获取一个有效的用户 ID
	var userID string
	err = db.SQL.QueryRowContext(ctx, "SELECT id::text FROM users LIMIT 1").Scan(&userID)
	if err != nil {
		t.Skipf("Skipping test, no user in users table: %v", err)
		return
	}

	// 2. 使用 Vault 对 TOTP Secret 进行 AES-256-GCM 自包含加密
	v, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatalf("NewMemoryVault error: %v", err)
	}

	sec, err := totp.GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret error: %v", err)
	}

	encRef, err := v.Encrypt(sec)
	if err != nil {
		t.Fatalf("Encrypt error: %v", err)
	}
	if !strings.HasPrefix(encRef, "enc:v1:") {
		t.Fatalf("expected enc:v1: prefix, got %s", encRef)
	}

	// 3. 落库持久化
	b := &approval.TOTPBinding{
		UserID:    userID,
		SecretRef: encRef,
		Enabled:   true,
	}
	if err := store.Save(ctx, b); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	// 4. 从 DB 读取并验证密文落盘
	got, err := store.Get(ctx, userID)
	if err != nil {
		t.Fatalf("Get error: %v", err)
	}
	if got == nil || !got.Enabled || got.SecretRef != encRef {
		t.Fatalf("unexpected binding: %+v", got)
	}

	// 5. 验证模拟容器重启（全新 MemoryVault 实例，读取同一主密钥）仍能成功解密并校验口令
	v2, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatalf("NewMemoryVault v2 error: %v", err)
	}
	decryptedSecret, err := v2.Get(got.SecretRef)
	if err != nil {
		t.Fatalf("v2.Get decrypt error: %v", err)
	}
	if decryptedSecret != sec {
		t.Fatalf("decrypted secret mismatch: expected %s, got %s", sec, decryptedSecret)
	}

	// 6. 验证生成的 TOTP code 校验通过
	code, err := totp.CodeAt(decryptedSecret, time.Now())
	if err != nil {
		t.Fatalf("CodeAt error: %v", err)
	}
	if !totp.Verify(decryptedSecret, code, time.Now()) {
		t.Fatalf("totp verification failed for code %s", code)
	}
}
