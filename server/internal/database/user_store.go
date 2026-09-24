package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
)

// PostgresUserStore 基于 PostgreSQL 实现 auth.UserStore。
type PostgresUserStore struct {
	db *DB
}

func (s *PostgresUserStore) SeedAdmin(username, password, display string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	existing, _ := s.FindByUsername(ctx, username)
	if existing != nil {
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	return s.Create(ctx, &auth.User{
		ID:           newUUID(),
		Username:     username,
		PasswordHash: hash,
		DisplayName:  display,
		Status:       auth.StatusActive,
		Roles:        []string{"ADMIN"},
	})
}

func scanUser(row interface {
	Scan(dest ...any) error
}) (*auth.User, error) {
	var u auth.User
	var lockedUntil, lastLogin sql.NullTime
	var email, status sql.NullString
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.FailedAttempts, &lockedUntil, &email, &status, &lastLogin, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if lockedUntil.Valid {
		u.LockedUntil = lockedUntil.Time
	}
	if lastLogin.Valid {
		u.LastLoginAt = lastLogin.Time
	}
	u.Email = email.String
	u.Status = status.String
	if u.Status == "" {
		u.Status = auth.StatusActive
	}
	return &u, nil
}

func (s *PostgresUserStore) loadRoles(ctx context.Context, u *auth.User) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT r.name FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1::uuid`, u.ID)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err == nil {
			u.Roles = append(u.Roles, r)
		}
	}
	if len(u.Roles) == 0 {
		u.Roles = []string{"VIEWER"}
	}
}

func (s *PostgresUserStore) FindByUsername(ctx context.Context, username string) (*auth.User, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, failed_attempts, locked_until,
		       COALESCE(email,''), COALESCE(status,'active'), last_login_at, created_at, updated_at
		FROM users WHERE username = $1 AND deleted_at IS NULL`, username)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		// 兼容未迁移列
		return s.findByUsernameLegacy(ctx, username)
	}
	s.loadRoles(ctx, u)
	return u, nil
}

func (s *PostgresUserStore) findByUsernameLegacy(ctx context.Context, username string) (*auth.User, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, failed_attempts, locked_until, created_at, updated_at
		FROM users WHERE username = $1`, username)
	var u auth.User
	var lockedUntil sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.FailedAttempts, &lockedUntil, &u.CreatedAt, &u.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lockedUntil.Valid {
		u.LockedUntil = lockedUntil.Time
	}
	u.Status = auth.StatusActive
	s.loadRoles(ctx, &u)
	return &u, nil
}

func (s *PostgresUserStore) FindByID(ctx context.Context, id string) (*auth.User, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, failed_attempts, locked_until,
		       COALESCE(email,''), COALESCE(status,'active'), last_login_at, created_at, updated_at
		FROM users WHERE id = $1::uuid AND deleted_at IS NULL`, id)
	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	s.loadRoles(ctx, u)
	return u, nil
}

func (s *PostgresUserStore) List(ctx context.Context) ([]*auth.User, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT id, username, password_hash, display_name, failed_attempts, locked_until,
		       COALESCE(email,''), COALESCE(status,'active'), last_login_at, created_at, updated_at
		FROM users WHERE deleted_at IS NULL ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*auth.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		s.loadRoles(ctx, u)
		list = append(list, u)
	}
	return list, nil
}

func (s *PostgresUserStore) Create(ctx context.Context, u *auth.User) error {
	if u.ID == "" || len(u.ID) < 32 {
		u.ID = newUUID()
	}
	if u.Status == "" {
		u.Status = auth.StatusActive
	}
	var lockedUntil sql.NullTime
	if !u.LockedUntil.IsZero() {
		lockedUntil = sql.NullTime{Time: u.LockedUntil, Valid: true}
	}
	var lastLogin sql.NullTime
	if !u.LastLoginAt.IsZero() {
		lastLogin = sql.NullTime{Time: u.LastLoginAt, Valid: true}
	}
	query := `
		INSERT INTO users (id, username, password_hash, display_name, failed_attempts, locked_until,
			email, status, last_login_at, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			display_name = EXCLUDED.display_name,
			failed_attempts = EXCLUDED.failed_attempts,
			locked_until = EXCLUDED.locked_until,
			email = EXCLUDED.email,
			status = EXCLUDED.status,
			last_login_at = EXCLUDED.last_login_at,
			updated_at = NOW()
		RETURNING id
	`
	var returnedID string
	err := s.db.SQL.QueryRowContext(ctx, query, u.ID, u.Username, u.PasswordHash, u.DisplayName, u.FailedAttempts, lockedUntil, u.Email, u.Status, lastLogin).Scan(&returnedID)
	if err != nil {
		// 兼容未迁移
		return s.createLegacy(ctx, u)
	}
	if returnedID != "" {
		u.ID = returnedID
	}
	roles := u.Roles
	if len(roles) == 0 {
		roles = []string{"VIEWER"}
	}
	return s.SetRoles(ctx, u.ID, roles)
}

func (s *PostgresUserStore) createLegacy(ctx context.Context, u *auth.User) error {
	var lockedUntil sql.NullTime
	if !u.LockedUntil.IsZero() {
		lockedUntil = sql.NullTime{Time: u.LockedUntil, Valid: true}
	}
	var returnedID string
	err := s.db.SQL.QueryRowContext(ctx, `
		INSERT INTO users (id, username, password_hash, display_name, failed_attempts, locked_until, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (username) DO UPDATE SET password_hash = EXCLUDED.password_hash, display_name = EXCLUDED.display_name, updated_at = NOW()
		RETURNING id`, u.ID, u.Username, u.PasswordHash, u.DisplayName, u.FailedAttempts, lockedUntil).Scan(&returnedID)
	if err != nil {
		return err
	}
	u.ID = returnedID
	return s.SetRoles(ctx, u.ID, u.Roles)
}

func (s *PostgresUserStore) Update(ctx context.Context, u *auth.User) error {
	var lockedUntil sql.NullTime
	if !u.LockedUntil.IsZero() {
		lockedUntil = sql.NullTime{Time: u.LockedUntil, Valid: true}
	}
	var lastLogin sql.NullTime
	if !u.LastLoginAt.IsZero() {
		lastLogin = sql.NullTime{Time: u.LastLoginAt, Valid: true}
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		UPDATE users
		SET password_hash = $2, display_name = $3, failed_attempts = $4, locked_until = $5,
		    email = $6, status = $7, last_login_at = $8, updated_at = NOW()
		WHERE username = $1`, u.Username, u.PasswordHash, u.DisplayName, u.FailedAttempts, lockedUntil, u.Email, u.Status, lastLogin)
	if err != nil {
		_, err = s.db.SQL.ExecContext(ctx, `
			UPDATE users SET password_hash = $2, display_name = $3, failed_attempts = $4, locked_until = $5, updated_at = NOW()
			WHERE username = $1`, u.Username, u.PasswordHash, u.DisplayName, u.FailedAttempts, lockedUntil)
	}
	return err
}

func (s *PostgresUserStore) SetRoles(ctx context.Context, userID string, roles []string) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return err
	}
	for _, role := range roles {
		_, _ = s.db.SQL.ExecContext(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1::uuid, id FROM roles WHERE name = $2
			ON CONFLICT DO NOTHING`, userID, role)
	}
	return nil
}

func (s *PostgresUserStore) ListPermissionGrants(ctx context.Context, roles []string) ([]auth.PermissionGrant, error) {
	for _, r := range roles {
		if r == "SUPER_ADMIN" {
			return []auth.PermissionGrant{{Code: "*", Scope: "ALL"}}, nil
		}
	}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT p.code, COALESCE(NULLIF(rp.scope,''), 'ALL')
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN roles r ON r.id = rp.role_id
		WHERE r.name = ANY($1)`, roles)
	if err != nil {
		// 无 scope 列时回退
		codes, err2 := s.ListPermissions(ctx, roles)
		if err2 != nil {
			return nil, err2
		}
		out := make([]auth.PermissionGrant, 0, len(codes))
		for _, c := range codes {
			scope := "OWN"
			for _, r := range roles {
				if r == "ADMIN" {
					scope = "ALL"
					break
				}
			}
			out = append(out, auth.PermissionGrant{Code: c, Scope: scope})
		}
		return out, nil
	}
	defer rows.Close()
	best := map[string]auth.PermissionGrant{}
	for rows.Next() {
		var g auth.PermissionGrant
		if err := rows.Scan(&g.Code, &g.Scope); err != nil {
			continue
		}
		g.Scope = strings.ToUpper(g.Scope)
		if prev, ok := best[g.Code]; ok {
			if scopeRank(g.Scope) > scopeRank(prev.Scope) {
				best[g.Code] = g
			}
			continue
		}
		best[g.Code] = g
	}
	if len(best) == 0 {
		codes := fallbackPermissions(roles)
		out := make([]auth.PermissionGrant, 0, len(codes))
		scope := "OWN"
		for _, r := range roles {
			if r == "ADMIN" {
				scope = "ALL"
				break
			}
		}
		for _, c := range codes {
			out = append(out, auth.PermissionGrant{Code: c, Scope: scope})
		}
		return out, nil
	}
	out := make([]auth.PermissionGrant, 0, len(best))
	for _, g := range best {
		out = append(out, g)
	}
	return out, nil
}

func scopeRank(s string) int {
	switch strings.ToUpper(s) {
	case "ALL":
		return 4
	case "ASSIGNED":
		return 3
	case "OWN":
		return 2
	default:
		return 1
	}
}

func (s *PostgresUserStore) ListPermissions(ctx context.Context, roles []string) ([]string, error) {
	grants, err := s.ListPermissionGrants(ctx, roles)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		out = append(out, g.Code)
	}
	return out, nil
}

func (s *PostgresUserStore) ListRoles(ctx context.Context) ([]auth.RoleInfo, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT name, description FROM roles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.RoleInfo
	for rows.Next() {
		var ri auth.RoleInfo
		if err := rows.Scan(&ri.Name, &ri.Description); err != nil {
			return nil, err
		}
		gs, _ := s.ListPermissionGrants(ctx, []string{ri.Name})
		ri.Grants = gs
		out = append(out, ri)
	}
	return out, nil
}

func (s *PostgresUserStore) ListAllPermissions(ctx context.Context) ([]auth.PermInfo, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `SELECT code, description FROM permissions ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []auth.PermInfo
	for rows.Next() {
		var p auth.PermInfo
		if err := rows.Scan(&p.Code, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *PostgresUserStore) SetRolePermissionScope(ctx context.Context, roleName, permCode, scope string) error {
	scope = strings.ToUpper(scope)
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO role_permissions (role_id, permission_id, scope)
		SELECT r.id, p.id, $3
		FROM roles r, permissions p
		WHERE r.name = $1 AND p.code = $2
		ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = EXCLUDED.scope`, roleName, permCode, scope)
	return err
}

func (s *PostgresUserStore) CreateRole(ctx context.Context, name, description string, grants []auth.PermissionGrant) error {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return errors.New("角色标识不能为空")
	}
	if description == "" {
		description = name
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM roles WHERE name = $1`, name).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return errors.New("角色已存在")
	}
	roleID := newUUID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO roles (id, name, description) VALUES ($1::uuid, $2, $3)`, roleID, name, description); err != nil {
		return err
	}
	for _, g := range grants {
		code := strings.TrimSpace(g.Code)
		if code == "" {
			continue
		}
		scope := strings.ToUpper(strings.TrimSpace(g.Scope))
		if scope == "" {
			scope = "NONE"
		}
		res, err := tx.ExecContext(ctx, `
			INSERT INTO role_permissions (role_id, permission_id, scope)
			SELECT $1::uuid, p.id, $3
			FROM permissions p WHERE p.code = $2
			ON CONFLICT (role_id, permission_id) DO UPDATE SET scope = EXCLUDED.scope`, roleID, code, scope)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return fmt.Errorf("未知权限码: %s", code)
		}
	}
	return tx.Commit()
}

func (s *PostgresUserStore) DeleteRole(ctx context.Context, name string) error {
	name = strings.ToUpper(strings.TrimSpace(name))
	if name == "" {
		return errors.New("角色标识不能为空")
	}
	if auth.IsBuiltInRole(name) {
		return errors.New("内置角色不可删除")
	}
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var roleID string
	if err := tx.QueryRowContext(ctx, `SELECT id::text FROM roles WHERE name = $1`, name).Scan(&roleID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("角色不存在")
		}
		return err
	}
	var usersUsing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_roles WHERE role_id = $1::uuid`, roleID).Scan(&usersUsing); err != nil {
		return err
	}
	if usersUsing > 0 {
		return errors.New("仍有用户使用该角色，请先调整用户角色")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = $1::uuid`, roleID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM roles WHERE id = $1::uuid`, roleID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresUserStore) SoftDelete(ctx context.Context, id string) error {
	res, err := s.db.SQL.ExecContext(ctx, `
		UPDATE users SET deleted_at = NOW(), status = 'disabled', updated_at = NOW()
		WHERE id = $1::uuid AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("用户不存在")
	}
	_, _ = s.db.SQL.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id = $1::uuid`, id)
	return nil
}

func (s *PostgresUserStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE deleted_at IS NULL`).Scan(&count)
	if err != nil {
		err = s.db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	}
	return count, err
}

func (s *PostgresUserStore) IsInitialized(ctx context.Context) (bool, error) {
	n, err := s.Count(ctx)
	return n > 0, err
}

func fallbackPermissions(roles []string) []string {
	perms := map[string][]string{
		"ADMIN": {
			"employee.read", "employee.write", "employee.delete",
			"workstation.read", "workstation.write",
			"workspace.read", "workspace.write",
			"session.read", "session.write",
			"job.read", "job.write", "job.cancel",
			"message.read", "message.write",
			"audit.read",
			"approval.read", "approval.approve",
			"secret.read", "secret.write",
			"system.read", "system.write",
			"enrollment.write",
			"workflow.read", "workflow.write", "workflow.delete", "workflow.grant",
			"automation.read", "automation.write",
			"user.read", "user.create", "user.update", "user.disable", "user.delete",
			"role.read", "role.create", "role.update", "role.delete",
			"quota.read", "quota.update",
		},
		"OPERATOR": {
			"employee.read", "employee.write", "workstation.read", "workspace.read",
			"session.read", "job.read", "job.write", "job.cancel",
			"message.read", "message.write", "approval.read",
			"workflow.read", "workflow.grant",
			"automation.read", "quota.read",
		},
		"VIEWER": {
			"employee.read", "workstation.read", "workspace.read",
			"session.read", "job.read", "message.read",
			"approval.read", "audit.read",
			"workflow.read",
			"automation.read",
		},
	}
	seen := map[string]struct{}{}
	var out []string
	for _, r := range roles {
		for _, p := range perms[r] {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			out = append(out, p)
		}
	}
	return out
}
