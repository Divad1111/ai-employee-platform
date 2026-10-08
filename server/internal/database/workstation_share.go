package database

import (
	"context"
	"database/sql"

	"github.com/ai-employee-platform/server/internal/workstation"
)

// PostgresShareStore 把公用标记和角色授权落在 PostgreSQL。
type PostgresShareStore struct {
	db *DB
}

func (db *DB) NewShareStore() workstation.ShareStore {
	return &PostgresShareStore{db: db}
}

func (s *PostgresShareStore) Get(ctx context.Context, workstationID string) (workstation.Share, error) {
	share := workstation.Share{WorkstationID: workstationID}
	var created sql.NullString
	err := s.db.SQL.QueryRowContext(ctx, `
		SELECT is_public, created_by_user_id::text
		FROM workstations WHERE id = $1`, workstationID).Scan(&share.IsPublic, &created)
	if err == sql.ErrNoRows {
		return share, nil
	}
	if err != nil {
		return share, err
	}
	if created.Valid {
		share.CreatedBy = created.String
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT role_name FROM workstation_role_grants WHERE workstation_id = $1 ORDER BY role_name`, workstationID)
	if err != nil {
		return share, err
	}
	defer rows.Close()
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return share, err
		}
		share.Roles = append(share.Roles, role)
	}
	return share, rows.Err()
}

func (s *PostgresShareStore) Save(ctx context.Context, share workstation.Share) error {
	if !share.IsPublic {
		share.Roles = nil
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		UPDATE workstations SET is_public = $2, updated_at = NOW() WHERE id = $1`, share.WorkstationID, share.IsPublic); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM workstation_role_grants WHERE workstation_id = $1`, share.WorkstationID); err != nil {
		return err
	}
	for _, role := range share.Roles {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO workstation_role_grants (workstation_id, role_name) VALUES ($1, $2)
			ON CONFLICT (workstation_id, role_name) DO NOTHING`, share.WorkstationID, role); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresShareStore) ListPublic(ctx context.Context) ([]workstation.Share, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT w.id, COALESCE(w.created_by_user_id::text, ''), COALESCE(g.role_name, '')
		FROM workstations w
		LEFT JOIN workstation_role_grants g ON g.workstation_id = w.id
		WHERE w.is_public = TRUE
		ORDER BY w.id, g.role_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*workstation.Share{}
	var order []string
	for rows.Next() {
		var id, created, role string
		if err := rows.Scan(&id, &created, &role); err != nil {
			return nil, err
		}
		item := byID[id]
		if item == nil {
			item = &workstation.Share{WorkstationID: id, IsPublic: true, CreatedBy: created}
			byID[id] = item
			order = append(order, id)
		}
		if role != "" {
			item.Roles = append(item.Roles, role)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]workstation.Share, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}
