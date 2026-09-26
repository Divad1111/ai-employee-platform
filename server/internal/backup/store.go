package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrNotFound          = errors.New("记录不存在")
	ErrDestinationInUse  = errors.New("该存储目标正被备份策略使用，无法直接删除")
	ErrPolicyAlreadyBusy = errors.New("该策略当前已有正在运行的备份任务，请勿重复执行")
)

// Store 备份模块数据库仓储接口
type Store interface {
	// Destinations
	CreateDestination(ctx context.Context, d *Destination) error
	GetDestination(ctx context.Context, id string) (*Destination, error)
	UpdateDestination(ctx context.Context, d *Destination) error
	DeleteDestination(ctx context.Context, id string) error
	ListDestinations(ctx context.Context) ([]Destination, error)
	UpdateDestinationTestStatus(ctx context.Context, id, status, msg string) error

	// Policies
	CreatePolicy(ctx context.Context, p *Policy) error
	GetPolicy(ctx context.Context, id string) (*Policy, error)
	UpdatePolicy(ctx context.Context, p *Policy) error
	DeletePolicy(ctx context.Context, id string) error
	ListPolicies(ctx context.Context) ([]Policy, error)
	SetPolicyEnabled(ctx context.Context, id string, enabled bool) error
	UpdatePolicyRunStatus(ctx context.Context, id string, lastRunAt time.Time, status string, nextRunAt *time.Time) error

	// Runs
	CreateRun(ctx context.Context, r *BackupRun) error
	GetRun(ctx context.Context, id string) (*BackupRun, error)
	GetRunByBackupID(ctx context.Context, backupID string) (*BackupRun, error)
	UpdateRun(ctx context.Context, r *BackupRun) error
	DeleteRun(ctx context.Context, id string) error
	ListRuns(ctx context.Context, limit, offset int, status, policyID string) ([]BackupRun, int64, error)
	CreateRunDestination(ctx context.Context, rd *RunDestination) error
	UpdateRunDestination(ctx context.Context, rd *RunDestination) error
	ListRunDestinations(ctx context.Context, runID string) ([]RunDestination, error)
	GetOverviewStats(ctx context.Context) (*OverviewStats, error)

	// Restore
	CreateRestoreJob(ctx context.Context, job *RestoreJob) error
	GetRestoreJob(ctx context.Context, id string) (*RestoreJob, error)
	UpdateRestoreJob(ctx context.Context, job *RestoreJob) error
	ListRestoreJobs(ctx context.Context) ([]RestoreJob, error)
}

// PostgresStore 数据库实现
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 初始化仓储
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

// Destination 实现

func (s *PostgresStore) CreateDestination(ctx context.Context, d *Destination) error {
	q := `
		INSERT INTO backup_destinations (
			id, name, type, config_encrypted, enabled, 
			last_test_at, last_test_status, last_test_message,
			created_by, updated_by, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);
	`
	now := time.Now().UTC()
	if d.CreatedAt.IsZero() {
		d.CreatedAt = now
	}
	d.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, q,
		d.ID, d.Name, d.Type, d.ConfigEncrypted, d.Enabled,
		d.LastTestAt, d.LastTestStatus, d.LastTestMessage,
		d.CreatedBy, d.UpdatedBy, d.CreatedAt, d.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) GetDestination(ctx context.Context, id string) (*Destination, error) {
	q := `
		SELECT id, name, type, config_encrypted, enabled,
		       last_test_at, last_test_status, last_test_message,
		       created_by, updated_by, created_at, updated_at
		FROM backup_destinations WHERE id = $1;
	`
	var d Destination
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&d.ID, &d.Name, &d.Type, &d.ConfigEncrypted, &d.Enabled,
		&d.LastTestAt, &d.LastTestStatus, &d.LastTestMessage,
		&d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (s *PostgresStore) UpdateDestination(ctx context.Context, d *Destination) error {
	q := `
		UPDATE backup_destinations SET
			name = $2,
			type = $3,
			config_encrypted = CASE WHEN $4 != '' THEN $4 ELSE config_encrypted END,
			enabled = $5,
			updated_by = $6,
			updated_at = $7
		WHERE id = $1;
	`
	d.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, q,
		d.ID, d.Name, d.Type, d.ConfigEncrypted, d.Enabled, d.UpdatedBy, d.UpdatedAt,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteDestination(ctx context.Context, id string) error {
	// 校验是否被策略引用
	var inUse bool
	checkQ := `SELECT EXISTS(SELECT 1 FROM backup_policy_destinations WHERE destination_id = $1);`
	if err := s.db.QueryRowContext(ctx, checkQ, id).Scan(&inUse); err == nil && inUse {
		return ErrDestinationInUse
	}

	q := `DELETE FROM backup_destinations WHERE id = $1;`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return ErrDestinationInUse
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListDestinations(ctx context.Context) ([]Destination, error) {
	q := `
		SELECT id, name, type, config_encrypted, enabled,
		       last_test_at, last_test_status, last_test_message,
		       created_by, updated_by, created_at, updated_at
		FROM backup_destinations ORDER BY created_at ASC;
	`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Destination
	for rows.Next() {
		var d Destination
		if err := rows.Scan(
			&d.ID, &d.Name, &d.Type, &d.ConfigEncrypted, &d.Enabled,
			&d.LastTestAt, &d.LastTestStatus, &d.LastTestMessage,
			&d.CreatedBy, &d.UpdatedBy, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

func (s *PostgresStore) UpdateDestinationTestStatus(ctx context.Context, id, status, msg string) error {
	q := `
		UPDATE backup_destinations SET
			last_test_at = $2,
			last_test_status = $3,
			last_test_message = $4,
			updated_at = $2
		WHERE id = $1;
	`
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, q, id, now, status, msg)
	return err
}

// Policy 实现

func (s *PostgresStore) CreatePolicy(ctx context.Context, p *Policy) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := `
		INSERT INTO backup_policies (
			id, name, enabled, scope, schedule_type, cron_expression, timezone,
			retention_keep_last, compression_algorithm, compression_level,
			encryption_enabled, encryption_algorithm, encryption_key_version,
			next_run_at, last_run_at, last_run_status,
			created_by, updated_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10,
			$11, $12, $13,
			$14, $15, $16,
			$17, $18, $19, $20
		);
	`
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now

	_, err = tx.ExecContext(ctx, q,
		p.ID, p.Name, p.Enabled, p.Scope, p.ScheduleType, p.CronExpression, p.Timezone,
		p.RetentionKeepLast, p.CompressionAlgorithm, p.CompressionLevel,
		p.EncryptionEnabled, p.EncryptionAlgorithm, p.EncryptionKeyVersion,
		p.NextRunAt, p.LastRunAt, p.LastRunStatus,
		p.CreatedBy, p.UpdatedBy, p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return err
	}

	for _, destID := range p.DestinationIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO backup_policy_destinations (policy_id, destination_id)
			VALUES ($1, $2) ON CONFLICT DO NOTHING;
		`, p.ID, destID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *PostgresStore) GetPolicy(ctx context.Context, id string) (*Policy, error) {
	q := `
		SELECT id, name, enabled, scope, schedule_type, cron_expression, timezone,
		       retention_keep_last, compression_algorithm, compression_level,
		       encryption_enabled, encryption_algorithm, encryption_key_version,
		       next_run_at, last_run_at, last_run_status,
		       created_by, updated_by, created_at, updated_at
		FROM backup_policies WHERE id = $1;
	`
	var p Policy
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&p.ID, &p.Name, &p.Enabled, &p.Scope, &p.ScheduleType, &p.CronExpression, &p.Timezone,
		&p.RetentionKeepLast, &p.CompressionAlgorithm, &p.CompressionLevel,
		&p.EncryptionEnabled, &p.EncryptionAlgorithm, &p.EncryptionKeyVersion,
		&p.NextRunAt, &p.LastRunAt, &p.LastRunStatus,
		&p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// 加载 destinations
	dRows, err := s.db.QueryContext(ctx, `
		SELECT destination_id FROM backup_policy_destinations WHERE policy_id = $1 ORDER BY created_at ASC;
	`, p.ID)
	if err == nil {
		defer dRows.Close()
		for dRows.Next() {
			var dID string
			if err := dRows.Scan(&dID); err == nil {
				p.DestinationIDs = append(p.DestinationIDs, dID)
			}
		}
	}

	return &p, nil
}

func (s *PostgresStore) UpdatePolicy(ctx context.Context, p *Policy) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	q := `
		UPDATE backup_policies SET
			name = $2,
			enabled = $3,
			scope = $4,
			schedule_type = $5,
			cron_expression = $6,
			timezone = $7,
			retention_keep_last = $8,
			compression_algorithm = $9,
			compression_level = $10,
			encryption_enabled = $11,
			encryption_algorithm = $12,
			encryption_key_version = $13,
			next_run_at = $14,
			updated_by = $15,
			updated_at = $16
		WHERE id = $1;
	`
	p.UpdatedAt = time.Now().UTC()
	res, err := tx.ExecContext(ctx, q,
		p.ID, p.Name, p.Enabled, p.Scope, p.ScheduleType, p.CronExpression, p.Timezone,
		p.RetentionKeepLast, p.CompressionAlgorithm, p.CompressionLevel,
		p.EncryptionEnabled, p.EncryptionAlgorithm, p.EncryptionKeyVersion,
		p.NextRunAt, p.UpdatedBy, p.UpdatedAt,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}

	// 重新关联目标
	if _, err := tx.ExecContext(ctx, `DELETE FROM backup_policy_destinations WHERE policy_id = $1;`, p.ID); err != nil {
		return err
	}
	for _, destID := range p.DestinationIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO backup_policy_destinations (policy_id, destination_id)
			VALUES ($1, $2);
		`, p.ID, destID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *PostgresStore) DeletePolicy(ctx context.Context, id string) error {
	q := `DELETE FROM backup_policies WHERE id = $1;`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListPolicies(ctx context.Context) ([]Policy, error) {
	q := `
		SELECT id, name, enabled, scope, schedule_type, cron_expression, timezone,
		       retention_keep_last, compression_algorithm, compression_level,
		       encryption_enabled, encryption_algorithm, encryption_key_version,
		       next_run_at, last_run_at, last_run_status,
		       created_by, updated_by, created_at, updated_at
		FROM backup_policies ORDER BY created_at ASC;
	`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Policy
	for rows.Next() {
		var p Policy
		if err := rows.Scan(
			&p.ID, &p.Name, &p.Enabled, &p.Scope, &p.ScheduleType, &p.CronExpression, &p.Timezone,
			&p.RetentionKeepLast, &p.CompressionAlgorithm, &p.CompressionLevel,
			&p.EncryptionEnabled, &p.EncryptionAlgorithm, &p.EncryptionKeyVersion,
			&p.NextRunAt, &p.LastRunAt, &p.LastRunStatus,
			&p.CreatedBy, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	_ = rows.Close()

	for i := range list {
		dRows, err := s.db.QueryContext(ctx, `
			SELECT destination_id FROM backup_policy_destinations WHERE policy_id = $1;
		`, list[i].ID)
		if err == nil {
			for dRows.Next() {
				var dID string
				if err := dRows.Scan(&dID); err == nil {
					list[i].DestinationIDs = append(list[i].DestinationIDs, dID)
				}
			}
			_ = dRows.Close()
		}
	}

	return list, nil
}

func (s *PostgresStore) SetPolicyEnabled(ctx context.Context, id string, enabled bool) error {
	q := `UPDATE backup_policies SET enabled = $2, updated_at = NOW() WHERE id = $1;`
	res, err := s.db.ExecContext(ctx, q, id, enabled)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) UpdatePolicyRunStatus(ctx context.Context, id string, lastRunAt time.Time, status string, nextRunAt *time.Time) error {
	q := `
		UPDATE backup_policies SET
			last_run_at = $2,
			last_run_status = $3,
			next_run_at = $4,
			updated_at = NOW()
		WHERE id = $1;
	`
	_, err := s.db.ExecContext(ctx, q, id, lastRunAt, status, nextRunAt)
	return err
}

// Runs 实现

func (s *PostgresStore) CreateRun(ctx context.Context, r *BackupRun) error {
	q := `
		INSERT INTO backup_runs (
			id, policy_id, policy_name, backup_id, scope, status, trigger_type,
			started_at, completed_at, duration_ms, artifact_size, file_count,
			checksum, manifest_json, error_code, error_message,
			created_by, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19
		);
	`
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	r.UpdatedAt = now

	mJSON := r.ManifestJSON
	if len(mJSON) == 0 {
		mJSON = json.RawMessage("{}")
	}

	_, err := s.db.ExecContext(ctx, q,
		r.ID, r.PolicyID, r.PolicyName, r.BackupID, r.Scope, r.Status, r.TriggerType,
		r.StartedAt, r.CompletedAt, r.DurationMS, r.ArtifactSize, r.FileCount,
		r.Checksum, mJSON, r.ErrorCode, r.ErrorMessage,
		r.CreatedBy, r.CreatedAt, r.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) GetRun(ctx context.Context, id string) (*BackupRun, error) {
	q := `
		SELECT id, policy_id, policy_name, backup_id, scope, status, trigger_type,
		       started_at, completed_at, duration_ms, artifact_size, file_count,
		       checksum, manifest_json, error_code, error_message,
		       created_by, created_at, updated_at
		FROM backup_runs WHERE id = $1;
	`
	var r BackupRun
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&r.ID, &r.PolicyID, &r.PolicyName, &r.BackupID, &r.Scope, &r.Status, &r.TriggerType,
		&r.StartedAt, &r.CompletedAt, &r.DurationMS, &r.ArtifactSize, &r.FileCount,
		&r.Checksum, &r.ManifestJSON, &r.ErrorCode, &r.ErrorMessage,
		&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	dests, _ := s.ListRunDestinations(ctx, r.ID)
	r.Destinations = dests
	return &r, nil
}

func (s *PostgresStore) GetRunByBackupID(ctx context.Context, backupID string) (*BackupRun, error) {
	q := `
		SELECT id, policy_id, policy_name, backup_id, scope, status, trigger_type,
		       started_at, completed_at, duration_ms, artifact_size, file_count,
		       checksum, manifest_json, error_code, error_message,
		       created_by, created_at, updated_at
		FROM backup_runs WHERE backup_id = $1;
	`
	var r BackupRun
	err := s.db.QueryRowContext(ctx, q, backupID).Scan(
		&r.ID, &r.PolicyID, &r.PolicyName, &r.BackupID, &r.Scope, &r.Status, &r.TriggerType,
		&r.StartedAt, &r.CompletedAt, &r.DurationMS, &r.ArtifactSize, &r.FileCount,
		&r.Checksum, &r.ManifestJSON, &r.ErrorCode, &r.ErrorMessage,
		&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	dests, _ := s.ListRunDestinations(ctx, r.ID)
	r.Destinations = dests
	return &r, nil
}

func (s *PostgresStore) UpdateRun(ctx context.Context, r *BackupRun) error {
	q := `
		UPDATE backup_runs SET
			status = $2,
			started_at = $3,
			completed_at = $4,
			duration_ms = $5,
			artifact_size = $6,
			file_count = $7,
			checksum = $8,
			manifest_json = $9,
			error_code = $10,
			error_message = $11,
			updated_at = NOW()
		WHERE id = $1;
	`
	mJSON := r.ManifestJSON
	if len(mJSON) == 0 {
		mJSON = json.RawMessage("{}")
	}

	res, err := s.db.ExecContext(ctx, q,
		r.ID, r.Status, r.StartedAt, r.CompletedAt, r.DurationMS,
		r.ArtifactSize, r.FileCount, r.Checksum, mJSON,
		r.ErrorCode, r.ErrorMessage,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) DeleteRun(ctx context.Context, id string) error {
	q := `DELETE FROM backup_runs WHERE id = $1;`
	res, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListRuns(ctx context.Context, limit, offset int, status, policyID string) ([]BackupRun, int64, error) {
	if limit <= 0 {
		limit = 20
	}
	where := "WHERE 1=1"
	var args []any
	argIdx := 1

	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if policyID != "" {
		where += fmt.Sprintf(" AND policy_id = $%d", argIdx)
		args = append(args, policyID)
		argIdx++
	}

	var total int64
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM backup_runs %s;", where)
	_ = s.db.QueryRowContext(ctx, countQ, args...).Scan(&total)

	q := fmt.Sprintf(`
		SELECT id, policy_id, policy_name, backup_id, scope, status, trigger_type,
		       started_at, completed_at, duration_ms, artifact_size, file_count,
		       checksum, manifest_json, error_code, error_message,
		       created_by, created_at, updated_at
		FROM backup_runs
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d;
	`, where, argIdx, argIdx+1)
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var list []BackupRun
	for rows.Next() {
		var r BackupRun
		if err := rows.Scan(
			&r.ID, &r.PolicyID, &r.PolicyName, &r.BackupID, &r.Scope, &r.Status, &r.TriggerType,
			&r.StartedAt, &r.CompletedAt, &r.DurationMS, &r.ArtifactSize, &r.FileCount,
			&r.Checksum, &r.ManifestJSON, &r.ErrorCode, &r.ErrorMessage,
			&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		list = append(list, r)
	}
	_ = rows.Close()

	for i := range list {
		dests, _ := s.ListRunDestinations(ctx, list[i].ID)
		list[i].Destinations = dests
	}

	return list, total, nil
}

func (s *PostgresStore) CreateRunDestination(ctx context.Context, rd *RunDestination) error {
	q := `
		INSERT INTO backup_run_destinations (
			id, backup_run_id, destination_id, status,
			remote_path, remote_size, uploaded_at, verified_at,
			error_code, error_message
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);
	`
	_, err := s.db.ExecContext(ctx, q,
		rd.ID, rd.BackupRunID, rd.DestinationID, rd.Status,
		rd.RemotePath, rd.RemoteSize, rd.UploadedAt, rd.VerifiedAt,
		rd.ErrorCode, rd.ErrorMessage,
	)
	return err
}

func (s *PostgresStore) UpdateRunDestination(ctx context.Context, rd *RunDestination) error {
	q := `
		UPDATE backup_run_destinations SET
			status = $2,
			remote_path = $3,
			remote_size = $4,
			uploaded_at = $5,
			verified_at = $6,
			error_code = $7,
			error_message = $8
		WHERE id = $1;
	`
	_, err := s.db.ExecContext(ctx, q,
		rd.ID, rd.Status, rd.RemotePath, rd.RemoteSize,
		rd.UploadedAt, rd.VerifiedAt, rd.ErrorCode, rd.ErrorMessage,
	)
	return err
}

func (s *PostgresStore) ListRunDestinations(ctx context.Context, runID string) ([]RunDestination, error) {
	q := `
		SELECT rd.id, rd.backup_run_id, rd.destination_id, d.name, d.type,
		       rd.status, rd.remote_path, rd.remote_size, rd.uploaded_at, rd.verified_at,
		       rd.error_code, rd.error_message
		FROM backup_run_destinations rd
		LEFT JOIN backup_destinations d ON d.id = rd.destination_id
		WHERE rd.backup_run_id = $1;
	`
	rows, err := s.db.QueryContext(ctx, q, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []RunDestination
	for rows.Next() {
		var rd RunDestination
		var name, dType sql.NullString
		if err := rows.Scan(
			&rd.ID, &rd.BackupRunID, &rd.DestinationID, &name, &dType,
			&rd.Status, &rd.RemotePath, &rd.RemoteSize, &rd.UploadedAt, &rd.VerifiedAt,
			&rd.ErrorCode, &rd.ErrorMessage,
		); err != nil {
			return nil, err
		}
		rd.DestName = name.String
		rd.DestType = dType.String
		list = append(list, rd)
	}
	return list, nil
}

func (s *PostgresStore) GetOverviewStats(ctx context.Context) (*OverviewStats, error) {
	stats := &OverviewStats{}

	_ = s.db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			COUNT(*) FILTER (WHERE status = 'SUCCESS'),
			COUNT(*) FILTER (WHERE status = 'FAILED'),
			COUNT(*) FILTER (WHERE status = 'PARTIAL_SUCCESS'),
			COALESCE(SUM(artifact_size), 0)
		FROM backup_runs;
	`).Scan(
		&stats.TotalRuns,
		&stats.SuccessRuns,
		&stats.FailedRuns,
		&stats.PartialRuns,
		&stats.TotalArtifactSize,
	)

	_ = s.db.QueryRowContext(ctx, `
		SELECT 
			COUNT(*),
			COUNT(*) FILTER (WHERE enabled = true)
		FROM backup_policies;
	`).Scan(&stats.TotalPolicies, &stats.ActivePolicies)

	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM backup_destinations;`).Scan(&stats.TotalDestinations)

	// 最近一次执行
	_ = s.db.QueryRowContext(ctx, `
		SELECT created_at, status FROM backup_runs ORDER BY created_at DESC LIMIT 1;
	`).Scan(&stats.LastRunAt, &stats.LastRunStatus)

	// 最近一次成功执行
	_ = s.db.QueryRowContext(ctx, `
		SELECT created_at FROM backup_runs WHERE status = 'SUCCESS' ORDER BY created_at DESC LIMIT 1;
	`).Scan(&stats.LastSuccessRunAt)

	// 下次定时执行时间
	_ = s.db.QueryRowContext(ctx, `
		SELECT MIN(next_run_at) FROM backup_policies WHERE enabled = true AND next_run_at > NOW();
	`).Scan(&stats.NextScheduledAt)

	return stats, nil
}

// Restore 实现

func (s *PostgresStore) CreateRestoreJob(ctx context.Context, job *RestoreJob) error {
	q := `
		INSERT INTO backup_restore_jobs (
			id, backup_run_id, emergency_backup_run_id, status,
			started_at, completed_at, created_by, error_code, error_message,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
	`
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, q,
		job.ID, job.BackupRunID, job.EmergencyBackupRunID, job.Status,
		job.StartedAt, job.CompletedAt, job.CreatedBy, job.ErrorCode, job.ErrorMessage,
		job.CreatedAt, job.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) GetRestoreJob(ctx context.Context, id string) (*RestoreJob, error) {
	q := `
		SELECT id, backup_run_id, emergency_backup_run_id, status,
		       started_at, completed_at, created_by, error_code, error_message,
		       created_at, updated_at
		FROM backup_restore_jobs WHERE id = $1;
	`
	var job RestoreJob
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&job.ID, &job.BackupRunID, &job.EmergencyBackupRunID, &job.Status,
		&job.StartedAt, &job.CompletedAt, &job.CreatedBy, &job.ErrorCode, &job.ErrorMessage,
		&job.CreatedAt, &job.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &job, nil
}

func (s *PostgresStore) UpdateRestoreJob(ctx context.Context, job *RestoreJob) error {
	q := `
		UPDATE backup_restore_jobs SET
			status = $2,
			started_at = $3,
			completed_at = $4,
			error_code = $5,
			error_message = $6,
			updated_at = NOW()
		WHERE id = $1;
	`
	res, err := s.db.ExecContext(ctx, q,
		job.ID, job.Status, job.StartedAt, job.CompletedAt,
		job.ErrorCode, job.ErrorMessage,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListRestoreJobs(ctx context.Context) ([]RestoreJob, error) {
	q := `
		SELECT id, backup_run_id, emergency_backup_run_id, status,
		       started_at, completed_at, created_by, error_code, error_message,
		       created_at, updated_at
		FROM backup_restore_jobs ORDER BY created_at DESC LIMIT 50;
	`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []RestoreJob
	for rows.Next() {
		var job RestoreJob
		if err := rows.Scan(
			&job.ID, &job.BackupRunID, &job.EmergencyBackupRunID, &job.Status,
			&job.StartedAt, &job.CompletedAt, &job.CreatedBy, &job.ErrorCode, &job.ErrorMessage,
			&job.CreatedAt, &job.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, job)
	}
	return list, rows.Err()
}
