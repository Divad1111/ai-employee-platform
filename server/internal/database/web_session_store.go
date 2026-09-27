package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresWebSessionStore 在 PostgreSQL 中持久化管理员 Web 登录会话。
type PostgresWebSessionStore struct {
	db *DB
}

func (s *PostgresWebSessionStore) Save(ctx context.Context, sess *auth.Session) error {
	var stepUp sql.NullTime
	if !sess.StepUpUntil.IsZero() {
		stepUp = sql.NullTime{Time: sess.StepUpUntil, Valid: true}
	}
	roles := sess.Roles
	if roles == nil {
		roles = []string{}
	}
	query := `
		INSERT INTO admin_web_sessions (token, user_id, username, roles, expires_at, step_up_until)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (token) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			username = EXCLUDED.username,
			roles = EXCLUDED.roles,
			expires_at = EXCLUDED.expires_at,
			step_up_until = EXCLUDED.step_up_until
	`
	_, err := s.db.SQL.ExecContext(ctx, query, sess.Token, sess.UserID, sess.Username, roles, sess.ExpiresAt, stepUp)
	return err
}

func (s *PostgresWebSessionStore) Get(ctx context.Context, token string) (*auth.Session, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT token, user_id, username, roles, expires_at, step_up_until
		FROM admin_web_sessions
		WHERE token = $1`, token)
	var sess auth.Session
	var stepUp sql.NullTime
	var roles []string
	// pgx/stdlib 下 text[] 需经 Map.SQLScanner 才能扫入 []string
	tm := pgtype.NewMap()
	if err := row.Scan(&sess.Token, &sess.UserID, &sess.Username, tm.SQLScanner(&roles), &sess.ExpiresAt, &stepUp); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if stepUp.Valid {
		sess.StepUpUntil = stepUp.Time
	}
	sess.Roles = roles
	if len(sess.Roles) == 0 {
		// 历史脏数据 roles 列为空时回退为最小权限 VIEWER，避免赋予意外的高权限 ADMIN
		sess.Roles = []string{"VIEWER"}
	}
	return &sess, nil
}

func (s *PostgresWebSessionStore) Delete(ctx context.Context, token string) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM admin_web_sessions WHERE token = $1`, token)
	return err
}
