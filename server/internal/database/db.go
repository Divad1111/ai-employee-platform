package database

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/session"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// DB 封装 PostgreSQL 连接与仓储构造。
type DB struct {
	SQL *sql.DB
}

// Open 打开数据库连接并验证连通性。
func Open(databaseURL string) (*DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping 数据库失败: %w", err)
	}

	d := &DB{SQL: db}
	if err := d.ensureHelperTables(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("初始化辅助表失败: %w", err)
	}
	return d, nil
}

func (db *DB) Close() error {
	return db.SQL.Close()
}

// ensureHelperTables 保证 Web 会话持久化表等辅助表结构存在。
func (db *DB) ensureHelperTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS admin_web_sessions (
		token         TEXT PRIMARY KEY,
		user_id       TEXT NOT NULL,
		username      TEXT NOT NULL,
		roles         TEXT[] NOT NULL DEFAULT '{}',
		expires_at    TIMESTAMPTZ NOT NULL,
		step_up_until TIMESTAMPTZ
	);
	`
	_, err := db.SQL.Exec(schema)
	return err
}

func (db *DB) NewUserStore() auth.UserStore {
	return &PostgresUserStore{db: db}
}

func (db *DB) NewWebSessionStore() auth.SessionStore {
	return &PostgresWebSessionStore{db: db}
}

func (db *DB) NewEmployeeStore() employee.Store {
	return &PostgresEmployeeStore{db: db}
}

func (db *DB) NewWorkspaceStore() workspace.Store {
	return &PostgresWorkspaceStore{db: db}
}

func (db *DB) NewJobStore() job.Store {
	return &PostgresJobStore{db: db}
}

func (db *DB) NewSessionStore() session.Store {
	return &PostgresSessionStore{db: db}
}

func (db *DB) NewWorkstationMetaStore() workstation.MetaStore {
	return &PostgresWorkstationStore{db: db}
}

func (db *DB) NewCertStore() certca.CertificateStore {
	return &PostgresCertStore{db: db}
}

func (db *DB) NewTOTPStore() *PostgresTOTPStore {
	return &PostgresTOTPStore{db: db}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
