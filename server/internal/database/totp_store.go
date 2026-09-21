package database

import (
	"context"
	"database/sql"
	"errors"

	"github.com/ai-employee-platform/server/internal/approval"
)

// PostgresTOTPStore 在 PostgreSQL 中持久化用户 TOTP 绑定。
type PostgresTOTPStore struct {
	db *DB
}

func (s *PostgresTOTPStore) Get(ctx context.Context, userID string) (*approval.TOTPBinding, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT user_id::text, secret_ref, enabled, failed_count, locked_until
		FROM user_totp
		WHERE user_id = $1::uuid`, userID)
	var (
		b           approval.TOTPBinding
		lockedUntil sql.NullTime
	)
	if err := row.Scan(&b.UserID, &b.SecretRef, &b.Enabled, &b.FailedCount, &lockedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lockedUntil.Valid {
		b.LockedUntil = lockedUntil.Time
	}
	return &b, nil
}

func (s *PostgresTOTPStore) Save(ctx context.Context, b *approval.TOTPBinding) error {
	var locked sql.NullTime
	if !b.LockedUntil.IsZero() {
		locked = sql.NullTime{Time: b.LockedUntil, Valid: true}
	}
	query := `
		INSERT INTO user_totp (user_id, secret_ref, enabled, failed_count, locked_until, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			secret_ref = EXCLUDED.secret_ref,
			enabled = EXCLUDED.enabled,
			failed_count = EXCLUDED.failed_count,
			locked_until = EXCLUDED.locked_until,
			updated_at = NOW()
	`
	_, err := s.db.SQL.ExecContext(ctx, query, b.UserID, b.SecretRef, b.Enabled, b.FailedCount, locked)
	return err
}
