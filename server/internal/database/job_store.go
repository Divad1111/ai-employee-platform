package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/ai-employee-platform/server/internal/job"
)

// PostgresJobStore 基于 PostgreSQL 实现 job.Store。
type PostgresJobStore struct {
	db *DB
}

func (s *PostgresJobStore) Save(ctx context.Context, j *job.Job) error {
	now := time.Now().UTC()
	if j.CreatedAt.IsZero() {
		j.CreatedAt = now
	}
	var startedAt, completedAt sql.NullTime
	if !j.StartedAt.IsZero() {
		startedAt = sql.NullTime{Time: j.StartedAt, Valid: true}
	}
	if !j.CompletedAt.IsZero() {
		completedAt = sql.NullTime{Time: j.CompletedAt, Valid: true}
	}

	if j.WorkstationID != "" {
		_, _ = s.db.SQL.ExecContext(ctx, `INSERT INTO workstations (id, name, created_at, updated_at) VALUES ($1, $1, NOW(), NOW()) ON CONFLICT (id) DO NOTHING`, j.WorkstationID)
	}
	if j.SessionID != "" && j.EmployeeID != "" {
		_, _ = s.db.SQL.ExecContext(ctx, `INSERT INTO sessions (id, employee_id, created_at) VALUES ($1, $2, NOW()) ON CONFLICT (id) DO NOTHING`, j.SessionID, j.EmployeeID)
	}

	query := `
		INSERT INTO jobs (
			id, employee_id, workspace_id, session_id, workstation_id,
			prompt, created_by, status, result, idempotency_key, timeout_sec,
			created_at, started_at, completed_at
		) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (id) DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			session_id = EXCLUDED.session_id,
			workstation_id = EXCLUDED.workstation_id,
			status = EXCLUDED.status,
			result = EXCLUDED.result,
			started_at = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at
	`
	_, err := s.db.SQL.ExecContext(ctx, query,
		j.ID, j.EmployeeID, j.WorkspaceID, j.SessionID, j.WorkstationID,
		j.Prompt, j.CreatedBy, j.Status, j.Result, j.IdempotencyKey, j.TimeoutSec,
		j.CreatedAt, startedAt, completedAt,
	)
	return err
}

func (s *PostgresJobStore) Get(ctx context.Context, id string) (*job.Job, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, employee_id, COALESCE(workspace_id, ''), COALESCE(session_id, ''), COALESCE(workstation_id, ''),
		       prompt, created_by, status, result, idempotency_key, timeout_sec,
		       created_at, started_at, completed_at
		FROM jobs WHERE id = $1`, id)
	var j job.Job
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(&j.ID, &j.EmployeeID, &j.WorkspaceID, &j.SessionID, &j.WorkstationID,
		&j.Prompt, &j.CreatedBy, &j.Status, &j.Result, &j.IdempotencyKey, &j.TimeoutSec,
		&j.CreatedAt, &startedAt, &completedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, job.ErrNotFound
		}
		return nil, err
	}
	if startedAt.Valid {
		j.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		j.CompletedAt = completedAt.Time
	}
	return &j, nil
}

func (s *PostgresJobStore) GetByIdempotency(ctx context.Context, key string) (*job.Job, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, employee_id, COALESCE(workspace_id, ''), COALESCE(session_id, ''), COALESCE(workstation_id, ''),
		       prompt, created_by, status, result, idempotency_key, timeout_sec,
		       created_at, started_at, completed_at
		FROM jobs WHERE idempotency_key = $1`, key)
	var j job.Job
	var startedAt, completedAt sql.NullTime
	if err := row.Scan(&j.ID, &j.EmployeeID, &j.WorkspaceID, &j.SessionID, &j.WorkstationID,
		&j.Prompt, &j.CreatedBy, &j.Status, &j.Result, &j.IdempotencyKey, &j.TimeoutSec,
		&j.CreatedAt, &startedAt, &completedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if startedAt.Valid {
		j.StartedAt = startedAt.Time
	}
	if completedAt.Valid {
		j.CompletedAt = completedAt.Time
	}
	return &j, nil
}

func (s *PostgresJobStore) List(ctx context.Context) ([]*job.Job, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, employee_id, COALESCE(workspace_id, ''), COALESCE(session_id, ''), COALESCE(workstation_id, ''),
		       prompt, created_by, status, result, idempotency_key, timeout_sec,
		       created_at, started_at, completed_at
		FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*job.Job
	for rows.Next() {
		var j job.Job
		var startedAt, completedAt sql.NullTime
		if err := rows.Scan(&j.ID, &j.EmployeeID, &j.WorkspaceID, &j.SessionID, &j.WorkstationID,
			&j.Prompt, &j.CreatedBy, &j.Status, &j.Result, &j.IdempotencyKey, &j.TimeoutSec,
			&j.CreatedAt, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			j.StartedAt = startedAt.Time
		}
		if completedAt.Valid {
			j.CompletedAt = completedAt.Time
		}
		list = append(list, &j)
	}
	return list, nil
}

func (s *PostgresJobStore) AppendEvent(ctx context.Context, e *job.Event) error {
	payloadJSON, _ := json.Marshal(e.Payload)
	query := `INSERT INTO job_events (job_id, event_type, payload, created_at) VALUES ($1, $2, $3, $4) RETURNING id`
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	return s.db.SQL.QueryRowContext(ctx, query, e.JobID, e.EventType, payloadJSON, e.CreatedAt).Scan(&e.ID)
}

func (s *PostgresJobStore) ListEvents(ctx context.Context, jobID string) ([]*job.Event, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, job_id, event_type, payload, created_at
		FROM job_events WHERE job_id = $1 ORDER BY id ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*job.Event
	for rows.Next() {
		var e job.Event
		var rawPayload []byte
		if err := rows.Scan(&e.ID, &e.JobID, &e.EventType, &rawPayload, &e.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rawPayload, &e.Payload)
		list = append(list, &e)
	}
	return list, nil
}
