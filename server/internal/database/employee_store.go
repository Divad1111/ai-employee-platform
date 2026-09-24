package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
			workstation_id, workspace_id, permission_profile, status, owner_user_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, NULLIF($10, '')::uuid, $11, $12)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			role_summary = EXCLUDED.role_summary,
			default_provider = EXCLUDED.default_provider,
			workstation_id = EXCLUDED.workstation_id,
			workspace_id = EXCLUDED.workspace_id,
			permission_profile = EXCLUDED.permission_profile,
			status = EXCLUDED.status,
			owner_user_id = EXCLUDED.owner_user_id,
			updated_at = EXCLUDED.updated_at
	`
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	_, err := s.db.SQL.ExecContext(ctx, query,
		e.ID, e.Name, e.Description, e.RoleSummary, e.DefaultProvider,
		e.WorkstationID, e.WorkspaceID, e.PermissionProfile, e.Status, e.OwnerUserID, e.CreatedAt, e.UpdatedAt,
	)
	if err != nil {
		// 兼容未迁移 owner_user_id
		_, err = s.db.SQL.ExecContext(ctx, `
			INSERT INTO employees (
				id, name, description, role_summary, default_provider,
				workstation_id, workspace_id, permission_profile, status, created_at, updated_at
			) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, $9, $10, $11)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name, description = EXCLUDED.description, role_summary = EXCLUDED.role_summary,
				default_provider = EXCLUDED.default_provider, workstation_id = EXCLUDED.workstation_id,
				workspace_id = EXCLUDED.workspace_id, permission_profile = EXCLUDED.permission_profile,
				status = EXCLUDED.status, updated_at = EXCLUDED.updated_at`,
			e.ID, e.Name, e.Description, e.RoleSummary, e.DefaultProvider,
			e.WorkstationID, e.WorkspaceID, e.PermissionProfile, e.Status, e.CreatedAt, e.UpdatedAt)
	}
	return err
}

func (s *PostgresEmployeeStore) Get(ctx context.Context, id string) (*employee.Employee, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, name, description, role_summary, default_provider,
		       COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       permission_profile, status, COALESCE(owner_user_id::text, ''), created_at, updated_at
		FROM employees WHERE id = $1`, id)
	var e employee.Employee
	if err := row.Scan(&e.ID, &e.Name, &e.Description, &e.RoleSummary, &e.DefaultProvider,
		&e.WorkstationID, &e.WorkspaceID, &e.PermissionProfile, &e.Status, &e.OwnerUserID, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, employee.ErrNotFound
		}
		// 兼容未迁移
		row2 := s.db.SQL.QueryRowContext(ctx, `
			SELECT id, name, description, role_summary, default_provider,
			       COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
			       permission_profile, status, created_at, updated_at
			FROM employees WHERE id = $1`, id)
		if err2 := row2.Scan(&e.ID, &e.Name, &e.Description, &e.RoleSummary, &e.DefaultProvider,
			&e.WorkstationID, &e.WorkspaceID, &e.PermissionProfile, &e.Status, &e.CreatedAt, &e.UpdatedAt); err2 != nil {
			if errors.Is(err2, sql.ErrNoRows) {
				return nil, employee.ErrNotFound
			}
			return nil, err2
		}
	}
	return &e, nil
}

func (s *PostgresEmployeeStore) List(ctx context.Context) ([]*employee.Employee, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, name, description, role_summary, default_provider,
		       COALESCE(workstation_id, ''), COALESCE(workspace_id, ''),
		       permission_profile, status, COALESCE(owner_user_id::text, ''), created_at, updated_at
		FROM employees ORDER BY created_at ASC`)
	if err != nil {
		return s.listLegacy(ctx)
	}
	defer rows.Close()
	var list []*employee.Employee
	for rows.Next() {
		var e employee.Employee
		if err := rows.Scan(&e.ID, &e.Name, &e.Description, &e.RoleSummary, &e.DefaultProvider,
			&e.WorkstationID, &e.WorkspaceID, &e.PermissionProfile, &e.Status, &e.OwnerUserID, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, &e)
	}
	return list, nil
}

func (s *PostgresEmployeeStore) listLegacy(ctx context.Context) ([]*employee.Employee, error) {
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
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. 检查该数字员工是否有处于 RUNNING 或 DISPATCHED 的任务
	var runningCount int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE employee_id = $1 AND status IN ('RUNNING', 'DISPATCHED')`, id).Scan(&runningCount)
	if runningCount > 0 {
		return fmt.Errorf("无法删除数字员工：该员工当前仍有 %d 个正在执行的任务，请先取消任务或等待完成", runningCount)
	}

	// 2. 清理历史 jobs（包含 job_events 级联）与 sessions
	_, _ = tx.ExecContext(ctx, `DELETE FROM jobs WHERE employee_id = $1`, id)
	_, _ = tx.ExecContext(ctx, `DELETE FROM sessions WHERE employee_id = $1`, id)

	// 3. 解绑绑定的 workspaces（设置 employee_id 为 NULL）
	_, _ = tx.ExecContext(ctx, `UPDATE workspaces SET employee_id = NULL WHERE employee_id = $1`, id)

	// 4. 删除员工记录（自动级联清理 employee_skills, employee_knowledge, employee_feishu_bindings）
	res, err := tx.ExecContext(ctx, `DELETE FROM employees WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return employee.ErrNotFound
	}

	return tx.Commit()
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
