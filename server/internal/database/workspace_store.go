package database

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ai-employee-platform/gen/workspacedir"
	"time"

	"github.com/ai-employee-platform/server/internal/workspace"
)

// PostgresWorkspaceStore 基于 PostgreSQL 实现 workspace.Store。
type PostgresWorkspaceStore struct {
	db *DB
}

type workspaceExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *PostgresWorkspaceStore) Save(ctx context.Context, w *workspace.Workspace) error {
	return saveWorkspace(ctx, s.db.SQL, w)
}
func saveWorkspace(ctx context.Context, db workspaceExecutor, w *workspace.Workspace) error {
	query := `
		INSERT INTO workspaces (
			id, workstation_id, employee_id, path, repository, branch, created_at, updated_at,root_path,owner_user_id
		) VALUES ($1, NULLIF($2, ''), NULLIF($3, ''), $4, $5, $6, $7, $8,$9,NULLIF($10,'')::uuid)
		ON CONFLICT (id) DO UPDATE SET
			workstation_id = EXCLUDED.workstation_id,
			employee_id = EXCLUDED.employee_id,
			path = EXCLUDED.path, root_path=EXCLUDED.root_path,owner_user_id=EXCLUDED.owner_user_id,
			repository = EXCLUDED.repository,
			branch = EXCLUDED.branch,
			updated_at = EXCLUDED.updated_at
	`
	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	w.UpdatedAt = now
	_, err := db.ExecContext(ctx, query,
		w.ID, w.WorkstationID, w.EmployeeID, w.Path, w.Repository, w.Branch, w.CreatedAt, w.UpdatedAt, w.RootPath, w.OwnerUserID,
	)
	return err
}

func (s *PostgresWorkspaceStore) Get(ctx context.Context, id string) (*workspace.Workspace, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, COALESCE(workstation_id, ''), COALESCE(employee_id, ''), path, repository, branch, created_at, updated_at, root_path,COALESCE(owner_user_id::text,''),COALESCE(d.directory_name,'')
		FROM workspaces LEFT JOIN workspace_user_directories d ON d.user_id=workspaces.owner_user_id WHERE id = $1`, id)
	var w workspace.Workspace
	if err := row.Scan(&w.ID, &w.WorkstationID, &w.EmployeeID, &w.Path, &w.Repository, &w.Branch, &w.CreatedAt, &w.UpdatedAt, &w.RootPath, &w.OwnerUserID, &w.UserDirectory); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, workspace.ErrNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *PostgresWorkspaceStore) List(ctx context.Context) ([]*workspace.Workspace, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, COALESCE(workstation_id, ''), COALESCE(employee_id, ''), path, repository, branch, created_at, updated_at, root_path,COALESCE(owner_user_id::text,''),COALESCE(d.directory_name,'')
		FROM workspaces LEFT JOIN workspace_user_directories d ON d.user_id=workspaces.owner_user_id ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*workspace.Workspace
	for rows.Next() {
		var w workspace.Workspace
		if err := rows.Scan(&w.ID, &w.WorkstationID, &w.EmployeeID, &w.Path, &w.Repository, &w.Branch, &w.CreatedAt, &w.UpdatedAt, &w.RootPath, &w.OwnerUserID, &w.UserDirectory); err != nil {
			return nil, err
		}
		list = append(list, &w)
	}
	return list, rows.Err()
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

func (s *PostgresWorkspaceStore) UserDirectory(ctx context.Context, id, name string) (string, error) {
	var dir string
	err := s.db.SQL.QueryRowContext(ctx, "SELECT directory_name FROM workspace_user_directories WHERE user_id=$1", id).Scan(&dir)
	if err == nil {
		return dir, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if !workspacedir.ValidName(name) {
		return "", errors.New("账号名不能用作用户目录")
	}
	err = s.db.SQL.QueryRowContext(ctx, "INSERT INTO workspace_user_directories(user_id,directory_name) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET user_id=EXCLUDED.user_id RETURNING directory_name", id, name).Scan(&dir)
	return dir, err
}

func (s *PostgresWorkspaceStore) CreateManaged(ctx context.Context, w *workspace.Workspace, empID string) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := saveWorkspace(ctx, tx, w); err != nil {
		return err
	}
	if empID != "" {
		if err := bindManaged(ctx, tx, w, empID, false); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *PostgresWorkspaceStore) ChangeManagedBinding(ctx context.Context, w *workspace.Workspace, empID string, detach bool) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := bindManaged(ctx, tx, w, empID, detach); err != nil {
		return err
	}
	return tx.Commit()
}
func bindManaged(ctx context.Context, tx *sql.Tx, w *workspace.Workspace, empID string, detach bool) error {
	var ws string
	err := tx.QueryRowContext(ctx, "SELECT id FROM workspaces WHERE id=$1 AND owner_user_id=$2 AND workstation_id=$3 FOR KEY SHARE", w.ID, w.OwnerUserID, w.WorkstationID).Scan(&ws)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE employees
  SET workspace_id = CASE WHEN $4 THEN NULL ELSE $2 END, updated_at = NOW()
  WHERE id = $1 AND owner_user_id = $3
    AND (workstation_id IS NULL OR workstation_id = $5)
    AND (NOT $4 OR workspace_id = $2)`, empID, w.ID, w.OwnerUserID, detach, w.WorkstationID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if n != 1 {
		return errors.New("员工归属、工作站或当前绑定不匹配")
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE workspaces SET employee_id=NULL WHERE employee_id=$1", empID)
	return err
}
