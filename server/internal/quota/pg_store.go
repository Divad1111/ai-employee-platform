package quota

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresStore 基于 PostgreSQL 的配额存储。
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建。
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) UpsertPolicy(ctx context.Context, p *Policy) error {
	if p.PeriodType == "" {
		p.PeriodType = PeriodMonthly
	}
	var id string
	var created, updated time.Time
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO quota_policies (
			id, resource_type, resource_id, period_type,
			token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (resource_type, resource_id, period_type) DO UPDATE SET
			token_limit = EXCLUDED.token_limit,
			request_limit = EXCLUDED.request_limit,
			concurrency_limit = EXCLUDED.concurrency_limit,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()
		RETURNING id::text, created_at, updated_at`,
		p.ResourceType, p.ResourceID, p.PeriodType,
		p.TokenLimit, p.RequestLimit, p.ConcurrencyLimit, p.Enabled,
	).Scan(&id, &created, &updated)
	if err != nil {
		return err
	}
	p.ID = id
	p.CreatedAt = created
	p.UpdatedAt = updated
	return nil
}

func (s *PostgresStore) DeletePolicy(ctx context.Context, resourceType, resourceID, period string) error {
	if period == "" {
		period = PeriodMonthly
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM quota_policies
		WHERE resource_type = $1 AND resource_id = $2 AND period_type = $3`,
		resourceType, resourceID, period)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) GetPolicy(ctx context.Context, resourceType, resourceID, period string) (*Policy, error) {
	if period == "" {
		period = PeriodMonthly
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id::text, resource_type, resource_id, period_type,
		       token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at
		FROM quota_policies
		WHERE resource_type = $1 AND resource_id = $2 AND period_type = $3`,
		resourceType, resourceID, period)
	var p Policy
	err := row.Scan(&p.ID, &p.ResourceType, &p.ResourceID, &p.PeriodType,
		&p.TokenLimit, &p.RequestLimit, &p.ConcurrencyLimit, &p.Enabled, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PostgresStore) ListPolicies(ctx context.Context) ([]*Policy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id::text, resource_type, resource_id, period_type,
		       token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at
		FROM quota_policies
		ORDER BY resource_type, resource_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(&p.ID, &p.ResourceType, &p.ResourceID, &p.PeriodType,
			&p.TokenLimit, &p.RequestLimit, &p.ConcurrencyLimit, &p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpsertWSUserQuota(ctx context.Context, q *WSUserQuota) error {
	if q.PeriodType == "" {
		q.PeriodType = PeriodMonthly
	}
	var id string
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO workstation_user_quotas (
			id, workstation_id, user_id, period_type,
			token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at
		) VALUES (gen_random_uuid(), $1, $2::uuid, $3, $4, $5, $6, $7, NOW(), NOW())
		ON CONFLICT (workstation_id, user_id, period_type) DO UPDATE SET
			token_limit = EXCLUDED.token_limit,
			request_limit = EXCLUDED.request_limit,
			concurrency_limit = EXCLUDED.concurrency_limit,
			enabled = EXCLUDED.enabled,
			updated_at = NOW()
		RETURNING id::text`,
		q.WorkstationID, q.UserID, q.PeriodType,
		q.TokenLimit, q.RequestLimit, q.ConcurrencyLimit, q.Enabled,
	).Scan(&id)
	if err != nil {
		return err
	}
	q.ID = id
	return nil
}

func (s *PostgresStore) ListWSUserQuotas(ctx context.Context, workstationID string) ([]*WSUserQuota, error) {
	var rows *sql.Rows
	var err error
	if workstationID == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id::text, workstation_id, user_id::text, period_type,
			       token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at
			FROM workstation_user_quotas`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id::text, workstation_id, user_id::text, period_type,
			       token_limit, request_limit, concurrency_limit, enabled, created_at, updated_at
			FROM workstation_user_quotas WHERE workstation_id = $1`, workstationID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*WSUserQuota
	for rows.Next() {
		var q WSUserQuota
		if err := rows.Scan(&q.ID, &q.WorkstationID, &q.UserID, &q.PeriodType,
			&q.TokenLimit, &q.RequestLimit, &q.ConcurrencyLimit, &q.Enabled, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}

func (s *PostgresStore) AddUsage(ctx context.Context, resourceType, resourceID, period string, tokens, requests int64) (*Usage, error) {
	if period == "" {
		period = PeriodMonthly
	}
	pk := periodKey(period)
	var u Usage
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO quota_usage (id, resource_type, resource_id, period_type, period_key, tokens_used, requests_used, updated_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (resource_type, resource_id, period_type, period_key) DO UPDATE SET
			tokens_used = quota_usage.tokens_used + EXCLUDED.tokens_used,
			requests_used = quota_usage.requests_used + EXCLUDED.requests_used,
			updated_at = NOW()
		RETURNING resource_type, resource_id, period_type, period_key, tokens_used, requests_used`,
		resourceType, resourceID, period, pk, tokens, requests,
	).Scan(&u.ResourceType, &u.ResourceID, &u.PeriodType, &u.PeriodKey, &u.TokensUsed, &u.RequestsUsed)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PostgresStore) GetUsage(ctx context.Context, resourceType, resourceID, period string) (*Usage, error) {
	if period == "" {
		period = PeriodMonthly
	}
	pk := periodKey(period)
	var u Usage
	err := s.db.QueryRowContext(ctx, `
		SELECT resource_type, resource_id, period_type, period_key, tokens_used, requests_used
		FROM quota_usage
		WHERE resource_type = $1 AND resource_id = $2 AND period_type = $3 AND period_key = $4`,
		resourceType, resourceID, period, pk,
	).Scan(&u.ResourceType, &u.ResourceID, &u.PeriodType, &u.PeriodKey, &u.TokensUsed, &u.RequestsUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return &Usage{ResourceType: resourceType, ResourceID: resourceID, PeriodType: period, PeriodKey: pk}, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
