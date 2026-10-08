package tokenusage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// PostgresStore PostgreSQL 明细存储。
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

const runCols = `id, job_id, COALESCE(workstation_id,''), COALESCE(digital_employee_id,''),
	COALESCE(provider,''), COALESCE(provider_session_id,''), COALESCE(provider_run_id,''),
	COALESCE(input_tokens,0), COALESCE(cached_input_tokens,0), COALESCE(cache_write_input_tokens,0),
	COALESCE(cache_read_input_tokens,0), COALESCE(output_tokens,0), COALESCE(reasoning_output_tokens,0),
	COALESCE(total_tokens,0), COALESCE(usage_status,''), COALESCE(usage_source,''), created_at, updated_at`

func scanRun(sc interface{ Scan(dest ...any) error }) (*RunUsage, error) {
	var u RunUsage
	if err := sc.Scan(
		&u.ID, &u.JobID, &u.WorkstationID, &u.DigitalEmployeeID,
		&u.Provider, &u.ProviderSessionID, &u.ProviderRunID,
		&u.InputTokens, &u.CachedInputTokens, &u.CacheWriteInputTokens,
		&u.CacheReadInputTokens, &u.OutputTokens, &u.ReasoningOutputTokens,
		&u.TotalTokens, &u.UsageStatus, &u.UsageSource, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) Upsert(ctx context.Context, u *RunUsage) (*RunUsage, bool, error) {
	if u.ProviderRunID != "" {
		existing, err := s.GetByProviderRun(ctx, u.Provider, u.ProviderRunID)
		if err != nil {
			return nil, false, err
		}
		if existing != nil {
			u.ID = existing.ID
			u.CreatedAt = existing.CreatedAt
			u.UpdatedAt = time.Now().UTC()
			_, err := s.db.ExecContext(ctx, `
				UPDATE job_token_usage SET
					job_id=$2, workstation_id=$3, digital_employee_id=$4,
					provider_session_id=$5,
					input_tokens=$6, cached_input_tokens=$7, cache_write_input_tokens=$8,
					cache_read_input_tokens=$9, output_tokens=$10, reasoning_output_tokens=$11,
					total_tokens=$12, usage_status=$13, usage_source=$14, updated_at=$15
				WHERE id=$1`,
				u.ID, u.JobID, u.WorkstationID, u.DigitalEmployeeID,
				u.ProviderSessionID,
				u.InputTokens, u.CachedInputTokens, u.CacheWriteInputTokens,
				u.CacheReadInputTokens, u.OutputTokens, u.ReasoningOutputTokens,
				u.TotalTokens, u.UsageStatus, u.UsageSource, u.UpdatedAt,
			)
			return u, false, err
		}
	}
	if u.ID == "" {
		u.ID = idgen.New("TKU")
	}
	now := time.Now().UTC()
	u.CreatedAt = now
	u.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO job_token_usage (
			id, job_id, workstation_id, digital_employee_id,
			provider, provider_session_id, provider_run_id,
			input_tokens, cached_input_tokens, cache_write_input_tokens, cache_read_input_tokens,
			output_tokens, reasoning_output_tokens, total_tokens,
			usage_status, usage_source, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		u.ID, u.JobID, u.WorkstationID, u.DigitalEmployeeID,
		u.Provider, u.ProviderSessionID, u.ProviderRunID,
		u.InputTokens, u.CachedInputTokens, u.CacheWriteInputTokens, u.CacheReadInputTokens,
		u.OutputTokens, u.ReasoningOutputTokens, u.TotalTokens,
		u.UsageStatus, u.UsageSource, u.CreatedAt, u.UpdatedAt,
	)
	return u, true, err
}

func (s *PostgresStore) ListByJob(ctx context.Context, jobID string) ([]*RunUsage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+` FROM job_token_usage WHERE job_id=$1 ORDER BY created_at ASC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*RunUsage, 0)
	for rows.Next() {
		u, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetByProviderRun(ctx context.Context, provider, runID string) (*RunUsage, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM job_token_usage WHERE provider=$1 AND provider_run_id=$2`, provider, runID)
	u, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}
