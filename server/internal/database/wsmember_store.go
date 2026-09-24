package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ai-employee-platform/server/internal/wsmember"
)

// PostgresWSMemberStore workstation_users 表。
type PostgresWSMemberStore struct {
	db *DB
}

// NewWSMemberStore 创建。
func NewWSMemberStore(db *DB) *PostgresWSMemberStore {
	return &PostgresWSMemberStore{db: db}
}

func (s *PostgresWSMemberStore) Upsert(ctx context.Context, m *wsmember.Membership) error {
	if m.ID == "" {
		m.ID = newUUID()
	}
	if m.Status == "" {
		m.Status = wsmember.StatusActive
	}
	now := time.Now().UTC()
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO workstation_users (id, workstation_id, user_id, role, status, created_at, updated_at)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7)
		ON CONFLICT (workstation_id, user_id) DO UPDATE SET
			role = EXCLUDED.role, status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
		m.ID, m.WorkstationID, m.UserID, m.Role, m.Status, m.CreatedAt, m.UpdatedAt)
	return err
}

func (s *PostgresWSMemberStore) Remove(ctx context.Context, workstationID, userID string) error {
	_, err := s.db.SQL.ExecContext(ctx, `
		DELETE FROM workstation_users WHERE workstation_id = $1 AND user_id = $2::uuid`, workstationID, userID)
	return err
}

func (s *PostgresWSMemberStore) Get(ctx context.Context, workstationID, userID string) (*wsmember.Membership, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, workstation_id, user_id, role, status, created_at, updated_at
		FROM workstation_users WHERE workstation_id = $1 AND user_id = $2::uuid AND status = 'active'`, workstationID, userID)
	var m wsmember.Membership
	if err := row.Scan(&m.ID, &m.WorkstationID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, wsmember.ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

func (s *PostgresWSMemberStore) ListByUser(ctx context.Context, userID string) ([]*wsmember.Membership, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, workstation_id, user_id, role, status, created_at, updated_at
		FROM workstation_users WHERE user_id = $1::uuid AND status = 'active'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMembers(rows)
}

func (s *PostgresWSMemberStore) ListByWorkstation(ctx context.Context, workstationID string) ([]*wsmember.Membership, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, workstation_id, user_id, role, status, created_at, updated_at
		FROM workstation_users WHERE workstation_id = $1 AND status = 'active'`, workstationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMembers(rows)
}

func (s *PostgresWSMemberStore) HasAccess(ctx context.Context, workstationID, userID string) (bool, error) {
	_, err := s.Get(ctx, workstationID, userID)
	if err == wsmember.ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func scanMembers(rows *sql.Rows) ([]*wsmember.Membership, error) {
	var list []*wsmember.Membership
	for rows.Next() {
		var m wsmember.Membership
		if err := rows.Scan(&m.ID, &m.WorkstationID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, &m)
	}
	return list, nil
}
