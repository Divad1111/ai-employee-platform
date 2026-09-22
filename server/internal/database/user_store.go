package database

import (
	"context"
	"database/sql"
	"errors"
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
		Roles:        []string{"ADMIN"},
	})
}

func (s *PostgresUserStore) FindByUsername(ctx context.Context, username string) (*auth.User, error) {
	row := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, username, password_hash, display_name, failed_attempts, locked_until
		FROM users WHERE username = $1`, username)
	var u auth.User
	var lockedUntil sql.NullTime
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.DisplayName, &u.FailedAttempts, &lockedUntil); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if lockedUntil.Valid {
		u.LockedUntil = lockedUntil.Time
	}

	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT r.name FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1::uuid`, u.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var r string
			if err := rows.Scan(&r); err == nil {
				u.Roles = append(u.Roles, r)
			}
		}
	}
	if len(u.Roles) == 0 {
		u.Roles = []string{"ADMIN"}
	}
	return &u, nil
}

func (s *PostgresUserStore) Create(ctx context.Context, u *auth.User) error {
	if !isValidUUID(u.ID) {
		u.ID = newUUID()
	}
	var lockedUntil sql.NullTime
	if !u.LockedUntil.IsZero() {
		lockedUntil = sql.NullTime{Time: u.LockedUntil, Valid: true}
	}
	query := `
		INSERT INTO users (id, username, password_hash, display_name, failed_attempts, locked_until, created_at, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (username) DO UPDATE SET
			password_hash = EXCLUDED.password_hash,
			display_name = EXCLUDED.display_name,
			failed_attempts = EXCLUDED.failed_attempts,
			locked_until = EXCLUDED.locked_until,
			updated_at = NOW()
		RETURNING id
	`
	var returnedID string
	err := s.db.SQL.QueryRowContext(ctx, query, u.ID, u.Username, u.PasswordHash, u.DisplayName, u.FailedAttempts, lockedUntil).Scan(&returnedID)
	if err != nil {
		return err
	}
	if returnedID != "" {
		u.ID = returnedID
	}

	roles := u.Roles
	if len(roles) == 0 {
		roles = []string{"ADMIN"}
	}
	for _, role := range roles {
		_, _ = s.db.SQL.ExecContext(ctx, `
			INSERT INTO user_roles (user_id, role_id)
			SELECT $1::uuid, id FROM roles WHERE name = $2
			ON CONFLICT DO NOTHING`, u.ID, role)
	}
	return nil
}

func (s *PostgresUserStore) Update(ctx context.Context, u *auth.User) error {
	var lockedUntil sql.NullTime
	if !u.LockedUntil.IsZero() {
		lockedUntil = sql.NullTime{Time: u.LockedUntil, Valid: true}
	}
	query := `
		UPDATE users
		SET password_hash = $2, display_name = $3, failed_attempts = $4, locked_until = $5, updated_at = NOW()
		WHERE username = $1
	`
	_, err := s.db.SQL.ExecContext(ctx, query, u.Username, u.PasswordHash, u.DisplayName, u.FailedAttempts, lockedUntil)
	return err
}

func (s *PostgresUserStore) ListPermissions(ctx context.Context, roles []string) ([]string, error) {
	for _, r := range roles {
		if r == "SUPER_ADMIN" {
			return []string{"*"}, nil
		}
	}
	perms := map[string]struct{}{}
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT DISTINCT p.code
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		JOIN roles r ON r.id = rp.role_id
		WHERE r.name = ANY($1)`, roles)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var code string
			if err := rows.Scan(&code); err == nil {
				perms[code] = struct{}{}
			}
		}
	}
	if len(perms) == 0 {
		return fallbackPermissions(roles), nil
	}
	out := make([]string, 0, len(perms))
	for code := range perms {
		out = append(out, code)
	}
	return out, nil
}

func (s *PostgresUserStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
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
		},
		"OPERATOR": {
			"employee.read", "workstation.read", "workspace.read",
			"session.read", "job.read", "job.write", "job.cancel",
			"message.read", "message.write", "approval.read",
			"workflow.read", "workflow.grant",
		},
		"VIEWER": {
			"employee.read", "workstation.read", "workspace.read",
			"session.read", "job.read", "message.read",
			"approval.read", "system.read", "audit.read",
			"workflow.read",
		},
	}
	set := map[string]struct{}{}
	for _, r := range roles {
		if r == "SUPER_ADMIN" {
			return []string{"*"}
		}
		for _, p := range perms[r] {
			set[p] = struct{}{}
		}
	}
	res := make([]string, 0, len(set))
	for k := range set {
		res = append(res, k)
	}
	return res
}

func isValidUUID(u string) bool {
	if len(u) != 36 {
		return false
	}
	for i, c := range u {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}

