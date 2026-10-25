package enrollment

import (
	"context"
	"database/sql"
	"encoding/json"
)

type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }
func (s *PostgresStore) Save(ctx context.Context, t *Token) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO security_enrollment_tokens(id,token_hash,payload,expires_at) VALUES($1,$2,$3,$4)`, t.ID, t.Hash, data, t.ExpiresAt)
	return err
}
func (s *PostgresStore) FindByHash(ctx context.Context, hash string) (*Token, error) {
	var data []byte
	var used sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT payload,used_at FROM security_enrollment_tokens WHERE token_hash=$1`, hash).Scan(&data, &used)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	var t Token
	if err = json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	if used.Valid {
		t.UsedAt = used.Time
	}
	return &t, nil
}
func (s *PostgresStore) MarkUsed(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE security_enrollment_tokens SET used_at=NOW() WHERE id=$1 AND used_at IS NULL AND expires_at>NOW()`, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrTokenUsed
	}
	return nil
}
func (s *PostgresStore) Consume(ctx context.Context, hash, workstationID string) (*Token, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var data []byte
	err = tx.QueryRowContext(ctx, `UPDATE security_enrollment_tokens SET used_at=NOW() WHERE token_hash=$1 AND used_at IS NULL AND expires_at>NOW() RETURNING payload`, hash).Scan(&data)
	if err != nil {
		return nil, ErrTokenInvalid
	}
	var t Token
	if err = json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO security_node_reservations(workstation_id,token_id) SELECT $1,$2 WHERE NOT EXISTS(SELECT 1 FROM workstations WHERE id=$1) ON CONFLICT(workstation_id) DO NOTHING`, workstationID, t.ID)
	if err != nil {
		return nil, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n != 1 {
		return nil, ErrTokenInvalid
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &t, nil
}
