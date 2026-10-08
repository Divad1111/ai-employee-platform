package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

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
			created_at, started_at, completed_at,
			input_tokens, output_tokens, cached_input_tokens, cache_write_input_tokens,
			cache_read_input_tokens, reasoning_output_tokens, total_tokens,
			agent, token_source, token_usage_status, source
		) VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25)
		ON CONFLICT (id) DO UPDATE SET
			workspace_id = EXCLUDED.workspace_id,
			session_id = EXCLUDED.session_id,
			workstation_id = EXCLUDED.workstation_id,
			status = EXCLUDED.status,
			result = EXCLUDED.result,
			started_at = EXCLUDED.started_at,
			completed_at = EXCLUDED.completed_at,
			input_tokens = EXCLUDED.input_tokens,
			output_tokens = EXCLUDED.output_tokens,
			cached_input_tokens = EXCLUDED.cached_input_tokens,
			cache_write_input_tokens = EXCLUDED.cache_write_input_tokens,
			cache_read_input_tokens = EXCLUDED.cache_read_input_tokens,
			reasoning_output_tokens = EXCLUDED.reasoning_output_tokens,
			total_tokens = EXCLUDED.total_tokens,
			agent = EXCLUDED.agent,
			token_source = EXCLUDED.token_source,
			token_usage_status = EXCLUDED.token_usage_status,
			source = EXCLUDED.source
	`
	source := j.Source
	if source == "" {
		source = "web"
	}
	_, err := s.db.SQL.ExecContext(ctx, query,
		j.ID, j.EmployeeID, j.WorkspaceID, j.SessionID, j.WorkstationID,
		j.Prompt, j.CreatedBy, j.Status, j.Result, j.IdempotencyKey, j.TimeoutSec,
		j.CreatedAt, startedAt, completedAt,
		j.InputTokens, j.OutputTokens, j.CachedInputTokens, j.CacheWriteInputTokens,
		j.CacheReadInputTokens, j.ReasoningOutputTokens, j.TotalTokens,
		j.Agent, j.TokenSource, j.TokenUsageStatus, source,
	)
	return friendlyJobWriteErr(err)
}

func friendlyJobWriteErr(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" && pgErr.ConstraintName == "jobs_workspace_id_fkey" {
		return errors.New("工作区未登记，无法创建任务。请先在数字员工中绑定已登记的项目工作区")
	}
	return err
}

const jobSelectCols = `id, employee_id, COALESCE(workspace_id, ''), COALESCE(session_id, ''), COALESCE(workstation_id, ''),
		       prompt, created_by, status, result, idempotency_key, timeout_sec,
		       created_at, started_at, completed_at,
		       COALESCE(input_tokens, 0), COALESCE(output_tokens, 0),
		       COALESCE(cached_input_tokens, 0), COALESCE(cache_write_input_tokens, 0),
		       COALESCE(cache_read_input_tokens, 0), COALESCE(reasoning_output_tokens, 0),
		       COALESCE(total_tokens, 0),
		       COALESCE(agent, ''), COALESCE(token_source, ''), COALESCE(token_usage_status, ''), COALESCE(source, 'web')`

func scanJob(sc interface {
	Scan(dest ...any) error
}) (*job.Job, error) {
	var j job.Job
	var startedAt, completedAt sql.NullTime
	if err := sc.Scan(&j.ID, &j.EmployeeID, &j.WorkspaceID, &j.SessionID, &j.WorkstationID,
		&j.Prompt, &j.CreatedBy, &j.Status, &j.Result, &j.IdempotencyKey, &j.TimeoutSec,
		&j.CreatedAt, &startedAt, &completedAt,
		&j.InputTokens, &j.OutputTokens,
		&j.CachedInputTokens, &j.CacheWriteInputTokens,
		&j.CacheReadInputTokens, &j.ReasoningOutputTokens,
		&j.TotalTokens,
		&j.Agent, &j.TokenSource, &j.TokenUsageStatus, &j.Source); err != nil {
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

func (s *PostgresJobStore) Get(ctx context.Context, id string) (*job.Job, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT `+jobSelectCols+` FROM jobs WHERE id = $1`, id)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, job.ErrNotFound
	}
	return j, err
}

func (s *PostgresJobStore) GetByIdempotency(ctx context.Context, key string) (*job.Job, error) {
	row := s.db.SQL.QueryRowContext(ctx, `SELECT `+jobSelectCols+` FROM jobs WHERE idempotency_key = $1`, key)
	j, err := scanJob(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

func (s *PostgresJobStore) List(ctx context.Context) ([]*job.Job, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT `+jobSelectCols+` FROM jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*job.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
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
