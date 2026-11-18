package mcpauth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresStore PostgreSQL Token 存储。
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建 PG 存储。
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Save(ctx context.Context, t *Token) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_tokens (
			id, token_hash, subject_type, subject_id, scope, label,
			expires_at, revoked_at, last_used_at, created_by, created_at, service
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		t.ID, t.TokenHash, t.SubjectType, t.SubjectID, t.Scope, t.Label,
		nullTime(t.ExpiresAt), nullTime(t.RevokedAt), nullTime(t.LastUsedAt),
		t.CreatedBy, t.CreatedAt, t.Service,
	)
	return err
}

func (s *PostgresStore) GetByHash(ctx context.Context, hash string) (*Token, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, token_hash, subject_type, subject_id, scope, label,
		       expires_at, revoked_at, last_used_at, created_by, created_at, service
		FROM mcp_tokens WHERE token_hash = $1`, hash)
	return scanToken(row)
}

func (s *PostgresStore) GetByID(ctx context.Context, id string) (*Token, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, token_hash, subject_type, subject_id, scope, label,
		       expires_at, revoked_at, last_used_at, created_by, created_at, service
		FROM mcp_tokens WHERE id = $1`, id)
	return scanToken(row)
}

func (s *PostgresStore) ListBySubject(ctx context.Context, subjectType, subjectID string) ([]*Token, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, token_hash, subject_type, subject_id, scope, label,
		       expires_at, revoked_at, last_used_at, created_by, created_at, service
		FROM mcp_tokens WHERE subject_type = $1 AND subject_id = $2
		ORDER BY created_at DESC`, subjectType, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Update(ctx context.Context, t *Token) error {
	res, err := s.db.ExecContext(ctx, `
		UPDATE mcp_tokens SET
			scope=$2, label=$3, expires_at=$4, revoked_at=COALESCE(revoked_at,$5), last_used_at=$6
		WHERE id = $1`,
		t.ID, t.Scope, t.Label, nullTime(t.ExpiresAt), nullTime(t.RevokedAt), nullTime(t.LastUsedAt))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM mcp_tokens WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanToken(row scannable) (*Token, error) {
	var t Token
	var exp, rev, used sql.NullTime
	err := row.Scan(&t.ID, &t.TokenHash, &t.SubjectType, &t.SubjectID, &t.Scope, &t.Label,
		&exp, &rev, &used, &t.CreatedBy, &t.CreatedAt, &t.Service)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if exp.Valid {
		t.ExpiresAt = &exp.Time
	}
	if rev.Valid {
		t.RevokedAt = &rev.Time
	}
	if used.Valid {
		t.LastUsedAt = &used.Time
	}
	return &t, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

var _ Store = (*PostgresStore)(nil)

func (s *PostgresStore) GetExternalPolicy(ctx context.Context, userID string) (*ExternalPolicy, error) {
	p := &ExternalPolicy{UserID: userID}
	err := s.db.QueryRowContext(ctx, `
		SELECT can_write, collector_can_write, updated_by, updated_at FROM mcp_external_user_policies WHERE user_id=$1`,
		userID).Scan(&p.CanWrite, &p.CollectorCanWrite, &p.UpdatedBy, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	return p, err
}

func (s *PostgresStore) SetExternalPolicy(ctx context.Context, p *ExternalPolicy) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_external_user_policies (user_id, can_write, updated_by, updated_at, collector_can_write)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (user_id) DO UPDATE SET
			can_write=EXCLUDED.can_write, collector_can_write=EXCLUDED.collector_can_write,
			updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at`,
		p.UserID, p.CanWrite, p.UpdatedBy, p.UpdatedAt, p.CollectorCanWrite)
	return err
}

func (s *PostgresStore) SetExternalServicePolicy(ctx context.Context, userID, service string, canWrite bool, actorID string) (*ExternalPolicy, error) {
	column := "can_write"
	if service == Collector {
		column = "collector_can_write"
	}
	p := &ExternalPolicy{UserID: userID}
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO mcp_external_user_policies (user_id, `+column+`, updated_by, updated_at)
		VALUES ($1,$2,$3,now())
		ON CONFLICT (user_id) DO UPDATE SET `+column+`=EXCLUDED.`+column+`,
			updated_by=EXCLUDED.updated_by, updated_at=EXCLUDED.updated_at
		RETURNING can_write, collector_can_write, updated_by, updated_at`,
		userID, canWrite, actorID).Scan(&p.CanWrite, &p.CollectorCanWrite, &p.UpdatedBy, &p.UpdatedAt)
	return p, err
}
