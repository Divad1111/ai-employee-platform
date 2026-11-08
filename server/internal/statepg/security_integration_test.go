package statepg_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/database"
	"github.com/ai-employee-platform/server/internal/statepg"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Run only against a fresh, isolated database after all migrations.
func TestPostgresSecurityPersistence(t *testing.T) {
	dsn := os.Getenv("AIE_SECURITY_TEST_DSN")
	if dsn == "" {
		t.Skip("set AIE_SECURITY_TEST_DSN to a fresh migrated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	open := func() *sql.DB {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(8)
		t.Cleanup(func() { db.Close() })
		return db
	}
	first, second := open(), open()
	key := "regression:" + uuid.NewString()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			db := first
			if i%2 == 0 {
				db = second
			}
			ok, err := statepg.Claim(ctx, db, key, time.Minute)
			if err != nil {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("claim accepted %d times", accepted.Load())
	}
	if ok, err := statepg.Claim(ctx, open(), key, time.Minute); ok || err != nil {
		t.Fatalf("claim lost on reconnect: %v", err)
	}
	if _, err := first.ExecContext(ctx, `UPDATE security_replay_keys SET expires_at=NOW()-INTERVAL '1 second' WHERE key=$1`, key); err != nil {
		t.Fatal(err)
	}
	if ok, err := statepg.Claim(ctx, second, key, time.Minute); !ok || err != nil {
		t.Fatalf("expired claim rejected: %v", err)
	}
	actor := uuid.NewString()
	audit.NewPostgres(first).Log(ctx, "USER", actor, "security.regression", "success", "127.0.0.1", map[string]string{"test": key})
	if got := audit.NewPostgres(open()).Query(audit.Filter{Actor: actor}); len(got) != 1 || got[0].Metadata["test"] != key {
		t.Fatalf("audit did not persist: %v", got)
	}
	var admins atomic.Int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store := (&database.DB{SQL: second}).NewUserStore()
			err := store.(*database.PostgresUserStore).CreateInitialAdmin(ctx, &auth.User{ID: uuid.NewString(), Username: fmt.Sprintf("initial-%d", i), PasswordHash: "test", DisplayName: "test"})
			if err == nil {
				admins.Add(1)
			} else if !errors.Is(err, auth.ErrAlreadyInitialized) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if admins.Load() != 1 {
		t.Fatalf("created %d initial admins", admins.Load())
	}
}
