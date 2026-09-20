package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ai-employee-platform/server/internal/employee"
)

// PostgresEmployeeStore 基于 PostgreSQL 实现 employee.Store。
type PostgresEmployeeStore struct {
	db *DB
}

func (s *PostgresEmployeeStore) Save(ctx context.Context, e *employee.Employee) error {
	query := `
		INSERT INTO employees (
			id, name, description, role_summary, default_provider,
			workstation_id, workspace_id, permission_profile, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			role_summary = EXCLUDED.role_summary,
			default_provider = EXCLUDED.default_provider,
			workstation_id = EXCLUDED.workstation_id,
			workspace_id = EXCLUDED.workspace_id,
			permission_profile = EXCLUDED.permission_profile,
			status = EXCLUDED.status,
			updated_at = EXCLUDED.updated_at
	`
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	_, err := s.db.SQL.ExecContext(ctx, query,
		e.ID, e.Name, e.Description, e.RoleSummary, e.DefaultProvider,
		e.WorkstationID, e.WorkspaceID, e.PermissionProfile, e.Status, e.CreatedAt, e.UpdatedAt,
	)
	return err
}

func (s *PostgresEmployeeStore) Get(ctx context.Context, id string) (*employee.Employee, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, name, description, role_summary, default_provider,
		       COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       permission_profile, status, created_at, updated_at
		FROM employees WHERE id = $1`, id)
	var e employee.Employee
	if err := row.Scan(&e.ID, &e.Name, &e.Description, &e.RoleSummary, &e.DefaultProvider,
		&e.WorkstationID, &e.WorkspaceID, &e.PermissionProfile, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, employee.ErrNotFound
		}
		return nil, err
	}
	return &e, nil
}

func (s *PostgresEmployeeStore) List(ctx context.Context) ([]*employee.Employee, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, name, description, role_summary, default_provider,
		       COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       permission_profile, status, created_at, updated_at
		FROM employees ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*employee.Employee
	for rows.Next() {
		var e employee.Employee
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.RoleSummary, &e.DefaultProvider,
			&e.WorkstationID, &e.WorkspaceID, &e.PermissionProfile, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, &e)
	}
	return list, nil
}

func (s *PostgresEmployeeStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM employees WHERE id = $1`, id)
	return err
}

func (s *PostgresEmployeeStore) FindByWorkspace(ctx context.Context, workspaceID string) (*employee.Employee, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, name, description, role_summary, default_provider,
		       COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       permission_profile, status, created_at, updated_at
		FROM employees WHERE workspace_id = $1 LIMIT 1`, workspaceID)
	var e employee.Employee
	if err := row.Scan(&e.ID, &e.Name, &e.Description, &e.RoleSummary, &e.DefaultProvider,
		&e.WorkstationID, &e.WorkspaceID, &e.PermissionProfile, &e.Status, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &e, nil
}
