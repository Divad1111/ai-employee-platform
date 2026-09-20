package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ai-employee-platform/server/internal/session"
)

// PostgresSessionStore 基于 PostgreSQL 实现 session.Store。
type PostgresSessionStore struct {
	db *DB
}

func (s *PostgresSessionStore) Save(ctx context.Context, sess *session.Session) error {
	now := time.Now().UTC()
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = now
	}
	var startedAt, endedAt sql.NullTime
	if !sess.StartedAt.IsZero() {
		startedAt = sql.NullTime{Time: sess.StartedAt, Valid: true}
	}
	if !sess.EndedAt.IsZero() {
		endedAt = sql.NullTime{Time: sess.EndedAt, Valid: true}
	}

	if sess.WorkstationID != "" {
		_, _ = s.db.SQL.ExecContext(ctx, `INSERT INTO workstations (id, name, created_at, updated_at) VALUES ($1, $1, NOW(), NOW()) ON CONFLICT (id) DO NOTHING`, sess.WorkstationID)
	}

	query := `
		INSERT INTO sessions (
			id, employee_id, workstation_id, workspace_id, provider, process_id,
			status, created_at, started_at, ended_at
		) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), $5, $6, $7, $8, $9, $10)
		ON CONFLICT (id) DO UPDATE SET
			workstation_id = EXCLUDED.workstation_id,
			workspace_id = EXCLUDED.workspace_id,
			provider = EXCLUDED.provider,
			process_id = EXCLUDED.process_id,
			status = EXCLUDED.status,
			started_at = EXCLUDED.started_at,
			ended_at = EXCLUDED.ended_at
	`
	_, err := s.db.SQL.ExecContext(ctx, query,
		sess.ID, sess.EmployeeID, sess.WorkstationID, sess.WorkspaceID, sess.Provider, sess.ProcessID,
		sess.Status, sess.CreatedAt, startedAt, endedAt,
	)
	return err
}

func (s *PostgresSessionStore) Get(ctx context.Context, id string) (*session.Session, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, employee_id, COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       provider, COALESCE(process_id, 0), status, created_at, started_at, ended_at
		FROM sessions WHERE id = $1`, id)
	var sess session.Session
	var startedAt, endedAt sql.NullTime
	if err := row.Scan(&sess.ID, &sess.EmployeeID, &sess.WorkstationID, &sess.WorkspaceID,
		&sess.Provider, &sess.ProcessID, &sess.Status, &sess.CreatedAt, &startedAt, &endedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, session.ErrNotFound
		}
		return nil, err
	}
	if startedAt.Valid {
		sess.StartedAt = startedAt.Time
	}
	if endedAt.Valid {
		sess.EndedAt = endedAt.Time
	}
	return &sess, nil
}

func (s *PostgresSessionStore) List(ctx context.Context) ([]*session.Session, error) {
	return s.scanList(ctx, `
		SELECT id, employee_id, COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       provider, COALESCE(process_id, 0), status, created_at, started_at, ended_at
		FROM sessions ORDER BY created_at ASC`)
}

func (s *PostgresSessionStore) ListByEmployee(ctx context.Context, employeeID string) ([]*session.Session, error) {
	return s.scanList(ctx, `
		SELECT id, employee_id, COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       provider, COALESCE(process_id, 0), status, created_at, started_at, ended_at
		FROM sessions WHERE employee_id = $1 ORDER BY created_at ASC`, employeeID)
}

func (s *PostgresSessionStore) scanList(ctx context.Context, query string, args ...any) ([]*session.Session, error) {
	rows, err := s.db.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*session.Session
	for rows.Next() {
		var sess session.Session
		var startedAt, endedAt sql.NullTime
		if err := rows.Scan(&sess.ID, &sess.EmployeeID, &sess.WorkstationID, &sess.WorkspaceID,
			&sess.Provider, &sess.ProcessID, &sess.Status, &sess.CreatedAt, &startedAt, &endedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			sess.StartedAt = startedAt.Time
		}
		if endedAt.Valid {
			sess.EndedAt = endedAt.Time
		}
		list = append(list, &sess)
	}
	return list, nil
}
