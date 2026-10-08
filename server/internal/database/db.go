package database

import (
	"context"
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
	"github.com/ai-employee-platform/server/internal/wsmember"
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

// ensureHelperTables 保证 Web 会话持久化表等辅助表结构存在，并在存在 roles 表时自愈补全默认角色。
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
	if _, err := db.SQL.Exec(schema); err != nil {
		return err
	}

	// 自愈保护：若 roles 表存在，确保内置角色（尤其是默认 USER 角色）就绪
	_, _ = db.SQL.Exec(`
		INSERT INTO roles (id, name, description) VALUES
			('00000000-0000-0000-0000-000000000001', 'SUPER_ADMIN', '超级管理员'),
			('00000000-0000-0000-0000-000000000002', 'ADMIN', '管理员'),
			('00000000-0000-0000-0000-000000000003', 'OPERATOR', '操作员'),
			('00000000-0000-0000-0000-000000000004', 'VIEWER', '只读'),
			('00000000-0000-0000-0000-000000000005', 'USER', '普通用户')
		ON CONFLICT (name) DO UPDATE SET description = EXCLUDED.description;
	`)

	// 确保 USER 角色赋权全量 permissions 且 Scope 为 OWN
	_, _ = db.SQL.Exec(`
		INSERT INTO role_permissions (role_id, permission_id, scope)
		SELECT r.id, p.id, 'OWN'
		FROM roles r
		CROSS JOIN permissions p
		WHERE r.name = 'USER'
		ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = 'OWN';
	`)

	// 公用工作站与按角色授权（兼容尚未执行 goose 的环境）
	_, _ = db.SQL.Exec(`ALTER TABLE workstations ADD COLUMN IF NOT EXISTS is_public BOOLEAN NOT NULL DEFAULT FALSE`)
	_, _ = db.SQL.Exec(`
		CREATE TABLE IF NOT EXISTS workstation_role_grants (
			workstation_id TEXT NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
			role_name      TEXT NOT NULL,
			created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (workstation_id, role_name)
		)
	`)

	// Token Usage：Job 汇总缓存列 + Run 明细表（幂等自愈，兼容未跑 goose 的环境）
	_, _ = db.SQL.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cached_input_tokens BIGINT NOT NULL DEFAULT 0`)
	_, _ = db.SQL.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cache_write_input_tokens BIGINT NOT NULL DEFAULT 0`)
	_, _ = db.SQL.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS cache_read_input_tokens BIGINT NOT NULL DEFAULT 0`)
	_, _ = db.SQL.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS reasoning_output_tokens BIGINT NOT NULL DEFAULT 0`)
	_, _ = db.SQL.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS total_tokens BIGINT NOT NULL DEFAULT 0`)
	_, _ = db.SQL.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS token_usage_status TEXT NOT NULL DEFAULT ''`)
	_, _ = db.SQL.Exec(`
		CREATE TABLE IF NOT EXISTS job_token_usage (
			id                        TEXT PRIMARY KEY,
			job_id                    TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			workstation_id            TEXT NOT NULL DEFAULT '',
			digital_employee_id       TEXT NOT NULL DEFAULT '',
			provider                  TEXT NOT NULL DEFAULT '',
			provider_session_id       TEXT NOT NULL DEFAULT '',
			provider_run_id           TEXT NOT NULL DEFAULT '',
			input_tokens              BIGINT NOT NULL DEFAULT 0,
			cached_input_tokens       BIGINT NOT NULL DEFAULT 0,
			cache_write_input_tokens  BIGINT NOT NULL DEFAULT 0,
			cache_read_input_tokens   BIGINT NOT NULL DEFAULT 0,
			output_tokens             BIGINT NOT NULL DEFAULT 0,
			reasoning_output_tokens   BIGINT NOT NULL DEFAULT 0,
			total_tokens              BIGINT NOT NULL DEFAULT 0,
			usage_status              TEXT NOT NULL DEFAULT 'UNAVAILABLE',
			usage_source              TEXT NOT NULL DEFAULT '',
			created_at                TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at                TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	_, _ = db.SQL.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_job_token_usage_provider_run
		ON job_token_usage(provider, provider_run_id)
		WHERE provider_run_id IS NOT NULL AND provider_run_id <> ''
	`)
	_, _ = db.SQL.Exec(`CREATE INDEX IF NOT EXISTS idx_job_token_usage_job ON job_token_usage(job_id)`)
	return nil
}

func (db *DB) NewUserStore() auth.UserStore {
	return &PostgresUserStore{db: db}
}

func (db *DB) NewWSMemberStore() wsmember.Store {
	return NewWSMemberStore(db)
}

// EnsureWorkstationOwners 把已记下创建者、但还没有站内成员的工作站补成 OWNER。
// 创建者为空、且用过的注册令牌都来自同一个人时，把这些工作站归到这个人。
func (db *DB) EnsureWorkstationOwners(ctx context.Context) error {
	if db == nil || db.SQL == nil {
		return nil
	}
	_, err := db.SQL.ExecContext(ctx, `
		UPDATE workstations w
		SET created_by_user_id = c.created_by, updated_at = NOW()
		FROM (
			SELECT created_by
			FROM enrollment_tokens
			WHERE used_at IS NOT NULL AND created_by IS NOT NULL
			GROUP BY created_by
		) c
		WHERE w.created_by_user_id IS NULL
		  AND (SELECT COUNT(DISTINCT created_by) FROM enrollment_tokens WHERE used_at IS NOT NULL AND created_by IS NOT NULL) = 1
	`)
	if err != nil {
		return err
	}
	_, err = db.SQL.ExecContext(ctx, `
		INSERT INTO workstation_users (id, workstation_id, user_id, role, status, created_at, updated_at)
		SELECT gen_random_uuid(), w.id, w.created_by_user_id, 'OWNER', 'active', NOW(), NOW()
		FROM workstations w
		WHERE w.created_by_user_id IS NOT NULL
		ON CONFLICT (workstation_id, user_id) DO NOTHING
	`)
	return err
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
