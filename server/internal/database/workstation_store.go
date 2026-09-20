package database

import (
	"context"
)

// PostgresWorkstationStore 基于 PostgreSQL 实现 workstation.MetaStore。
type PostgresWorkstationStore struct {
	db *DB
}

func (s *PostgresWorkstationStore) Upsert(ctx context.Context, id, name string) error {
	query := `
		INSERT INTO workstations (id, name, created_at, updated_at)
		VALUES ($1, $2, NOW(), NOW())
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, updated_at = NOW()
	`
	_, err := s.db.SQL.ExecContext(ctx, query, id, name)
	return err
}

func (s *PostgresWorkstationStore) GetName(ctx context.Context, id string) string {
	var name string
	_ = s.db.SQL.QueryRowContext(ctx, `SELECT name FROM workstations WHERE id = $1`, id).Scan(&name)
	return name
}

func (s *PostgresWorkstationStore) ListIDs(ctx context.Context) []string {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT id FROM workstations ORDER BY created_at ASC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
