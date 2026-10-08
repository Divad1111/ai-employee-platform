package database

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-employee-platform/server/internal/certca"
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

// SetCreatedBy 记下发注册令牌的用户。已有创建者时不覆盖。
func (s *PostgresWorkstationStore) SetCreatedBy(ctx context.Context, id, userID string) error {
	if id == "" || userID == "" {
		return nil
	}
	_, err := s.db.SQL.ExecContext(ctx, `
		UPDATE workstations
		SET created_by_user_id = $2::uuid, updated_at = NOW()
		WHERE id = $1 AND created_by_user_id IS NULL`, id, userID)
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

func (s *PostgresWorkstationStore) Delete(ctx context.Context, id string) error {
	tx, err := s.db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. 检查该计算节点是否有处于 RUNNING 或 DISPATCHED 的任务
	var runningCount int
	_ = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs WHERE workstation_id = $1 AND status IN ('RUNNING', 'DISPATCHED')`, id).Scan(&runningCount)
	if runningCount > 0 {
		return fmt.Errorf("无法删除工作站：该计算节点当前仍有 %d 个正在执行的任务，请先取消或等待完成", runningCount)
	}

	// 2. 解除历史 jobs 与 sessions 的外键关联（workstation_id 置 NULL）
	_, _ = tx.ExecContext(ctx, `UPDATE jobs SET workstation_id = NULL WHERE workstation_id = $1`, id)
	_, _ = tx.ExecContext(ctx, `UPDATE sessions SET workstation_id = NULL WHERE workstation_id = $1`, id)

	// 3. 解除 employees 的算力绑定
	_, _ = tx.ExecContext(ctx, `UPDATE employees SET workstation_id = NULL WHERE workstation_id = $1`, id)

	// 4. 清理证书记录
	_, _ = tx.ExecContext(ctx, `DELETE FROM workstation_certificates WHERE workstation_id = $1`, id)

	// 5. 物理删除 workstation 记录
	if _, err := tx.ExecContext(ctx, `DELETE FROM workstations WHERE id = $1`, id); err != nil {
		return err
	}

	return tx.Commit()
}

// PostgresCertStore 基于 PostgreSQL 实现 certca.CertificateStore。
type PostgresCertStore struct {
	db *DB
}

func (s *PostgresCertStore) SaveRecord(ctx context.Context, r *certca.Record) error {
	if r == nil || r.WorkstationID == "" || r.Fingerprint == "" {
		return nil
	}
	// 确保关联的 workstation 存在，以满足外键约束
	_, _ = s.db.SQL.ExecContext(ctx, `
		INSERT INTO workstations (id, name, created_at, updated_at)
		VALUES ($1, $1, NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`, r.WorkstationID)

	query := `
		INSERT INTO workstation_certificates (id, workstation_id, fingerprint, certificate_pem, status, issued_at, expires_at, revoked_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (fingerprint) DO UPDATE SET
			status = EXCLUDED.status,
			revoked_at = EXCLUDED.revoked_at
	`
	var revokedAt *time.Time
	if !r.RevokedAt.IsZero() {
		revokedAt = &r.RevokedAt
	}
	var expiresAt *time.Time
	if !r.ExpiresAt.IsZero() {
		expiresAt = &r.ExpiresAt
	}
	_, err := s.db.SQL.ExecContext(ctx, query, r.WorkstationID, r.Fingerprint, r.CertPEM, r.Status, r.IssuedAt, expiresAt, revokedAt)
	return err
}

func (s *PostgresCertStore) ListRecords(ctx context.Context) ([]*certca.Record, error) {
	rows, err := s.db.SQL.QueryContext(ctx, `
		SELECT workstation_id, fingerprint, certificate_pem, status, issued_at, expires_at, revoked_at
		FROM workstation_certificates
		ORDER BY issued_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*certca.Record
	for rows.Next() {
		var r certca.Record
		var exp, rev *time.Time
		if err := rows.Scan(&r.WorkstationID, &r.Fingerprint, &r.CertPEM, &r.Status, &r.IssuedAt, &exp, &rev); err != nil {
			continue
		}
		if exp != nil {
			r.ExpiresAt = *exp
		}
		if rev != nil {
			r.RevokedAt = *rev
		}
		list = append(list, &r)
	}
	return list, nil
}
