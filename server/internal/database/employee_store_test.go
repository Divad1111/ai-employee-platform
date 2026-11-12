package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/google/uuid"
	"github.com/pressly/goose/v3"
)

func TestPostgresEmployeeDelete(t *testing.T) {
	dsn := os.Getenv("AIE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set AIE_TEST_DATABASE_URL to run the isolated PostgreSQL regression")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	schema := "employee_delete_" + uuid.New().String()[:8]
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE")
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	migrations := os.Getenv("AIE_TEST_MIGRATIONS_DIR")
	if migrations == "" {
		migrations = filepath.Join("..", "..", "..", "migrations")
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, migrations); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	count := func(query string, want int) {
		t.Helper()
		var got int
		if err := db.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got %d, want %d, error %v", query, got, want, err)
		}
	}
	store := &PostgresEmployeeStore{db: &DB{SQL: db}}
	exec(`INSERT INTO employees(id,name) VALUES('delete-me','A'),('keep-me','B')`)
	exec(`INSERT INTO workspaces(id,employee_id,path) VALUES('project','delete-me','/project')`)
	exec(`INSERT INTO sessions(id,employee_id) VALUES('session','delete-me')`)
	exec(`INSERT INTO jobs(id,employee_id,session_id,status,idempotency_key)
		VALUES('old-job','delete-me','session','FAILED','old-job')`)
	exec(`INSERT INTO jobs(id,employee_id,session_id,parent_job_id,status,idempotency_key)
		VALUES('other-job','keep-me','session','old-job','SUCCESS','other-job')`)
	exec(`INSERT INTO task_conversations(id,employee_id,current_job_id) VALUES('conversation','delete-me','old-job')`)
	exec(`INSERT INTO task_conversation_turns(conversation_id,job_id,turn_no) VALUES('conversation','old-job',1)`)
	exec(`INSERT INTO task_conversation_messages(conversation_id,job_id,role) VALUES('conversation','old-job','user')`)
	exec(`INSERT INTO approval_requests(id,job_id) VALUES($1,'old-job')`, uuid.New().String())

	for _, status := range []string{"ASSIGNED", "STARTING", "RUNNING", "BLOCKED", "WAITING_APPROVAL", "UNKNOWN"} {
		exec(`UPDATE jobs SET status=$1 WHERE id='old-job'`, status)
		if err := store.Delete(ctx, "delete-me"); err == nil {
			t.Fatalf("deleted an employee with a %s job", status)
		}
		count(`SELECT count(*) FROM task_conversations WHERE id='conversation'`, 1)
	}
	exec(`UPDATE jobs SET status='FAILED' WHERE id='old-job'`)
	// Force a late failure to verify that earlier cleanup is rolled back.
	exec(`CREATE TABLE deletion_guard(employee_id TEXT REFERENCES employees(id))`)
	exec(`INSERT INTO deletion_guard VALUES('delete-me')`)
	if err := store.Delete(ctx, "delete-me"); err == nil {
		t.Fatal("expected the additional reference to prevent deletion")
	}
	count(`SELECT count(*) FROM task_conversations WHERE id='conversation'`, 1)
	count(`SELECT count(*) FROM jobs WHERE id='old-job'`, 1)
	count(`SELECT count(*) FROM workspaces WHERE employee_id='delete-me'`, 1)
	exec(`DROP TABLE deletion_guard`)
	if err := store.Delete(ctx, "delete-me"); err != nil {
		t.Fatal(err)
	}
	count(`SELECT count(*) FROM employees WHERE id='delete-me'`, 0)
	count(`SELECT count(*) FROM jobs WHERE id='old-job'`, 0)
	count(`SELECT count(*) FROM sessions WHERE id='session'`, 0)
	count(`SELECT count(*) FROM task_conversations`, 0)
	count(`SELECT count(*) FROM task_conversation_messages`, 0)
	count(`SELECT count(*) FROM task_conversation_turns`, 0)
	count(`SELECT count(*) FROM workspaces WHERE id='project' AND employee_id IS NULL`, 1)
	count(`SELECT count(*) FROM approval_requests WHERE job_id IS NULL`, 1)
	count(`SELECT count(*) FROM employees WHERE id='keep-me'`, 1)
	count(`SELECT count(*) FROM jobs WHERE id='other-job' AND parent_job_id IS NULL AND session_id IS NULL`, 1)
	if err := store.Delete(ctx, "delete-me"); !errors.Is(err, employee.ErrNotFound) {
		t.Fatalf("second deletion: %v", err)
	}
}
