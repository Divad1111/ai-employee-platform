package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ai-employee-platform/server/internal/workspace"
)

// PostgresWorkspaceStore 基于 PostgreSQL 实现 workspace.Store。
type PostgresWorkspaceStore struct {
	db *DB
}

func (s *PostgresWorkspaceStore) Save(ctx context.Context, w *workspace.Workspace) error {
	query := `
		INSERT INTO workspaces (
			id, workstation_id, employee_id, path, repository, branch, created_at, updated_at
		) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO UPDATE SET
			workstation_id = EXCLUDED.workstation_id,
			employee_id = EXCLUDED.employee_id,
			path = EXCLUDED.path,
			repository = EXCLUDED.repository,
			branch = EXCLUDED.branch,
			updated_at = EXCLUDED.updated_at
	`
	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	w.UpdatedAt = now
	_, err := s.db.SQL.ExecContext(ctx, query,
		w.ID, w.WorkstationID, w.EmployeeID, w.Path, w.Repository, w.Branch, w.CreatedAt, w.UpdatedAt,
	)
	return err
}

func (s *PostgresWorkspaceStore) Get(ctx context.Context, id string) (*workspace.Workspace, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, COALESCE(workstation_id, ''), COALESCE(employee_id, ''), path, repository, branch, created_at, updated_at
		FROM workspaces WHERE id = $1`, id)
	var w workspace.Workspace
	if err := row.Scan(&w.ID, &w.WorkstationID, &w.EmployeeID, &w.Path, &w.Repository, &w.Branch, &w.CreatedAt, &w.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, workspace.ErrNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *PostgresWorkspaceStore) List(ctx context.Context) ([]*workspace.Workspace, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, COALESCE(workstation_id, ''), COALESCE(employee_id, ''), path, repository, branch, created_at, updated_at
		FROM workspaces ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*workspace.Workspace
	for rows.Next() {
		var w workspace.Workspace
		if err := rows.Scan(&w.ID, &w.WorkstationID, &w.EmployeeID, &w.Path, &w.Repository, &w.Branch, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, &w)
	}
	return list, nil
}

func (s *PostgresWorkspaceStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// 历史会话和任务保留，只解开指向本工作区的外键。
	if _, err := tx.ExecContext(ctx, `UPDATE employees SET workspace_id = NULL WHERE workspace_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET workspace_id = NULL WHERE workspace_id = $1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE jobs SET workspace_id = NULL WHERE workspace_id = $1`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM workspaces WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return workspace.ErrNotFound
	}
	return tx.Commit()
}
