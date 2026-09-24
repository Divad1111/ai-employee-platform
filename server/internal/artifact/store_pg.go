package artifact

import (
	"context"
	"database/sql"
	"errors"
)

// PostgresStore 制品元数据持久化。
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建。
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func scanArtifact(row interface{ Scan(dest ...any) error }) (*Artifact, error) {
	var a Artifact
	var uploadedBy, wsID sql.NullString
	err := row.Scan(&a.ID, &a.JobID, &a.Name, &a.Type, &a.SizeBytes, &a.SHA256, &a.Storage, &a.CreatedAt, &uploadedBy, &wsID)
	if err != nil {
		return nil, err
	}
	if uploadedBy.Valid {
		a.UploadedBy = uploadedBy.String
	}
	if wsID.Valid {
		a.WorkstationID = wsID.String
	}
	return &a, nil
}

const artCols = `id, job_id, name, type, size_bytes, sha256, storage, created_at,
  COALESCE(uploaded_by,''), COALESCE(workstation_id,'')`

func (p *PostgresStore) Save(ctx context.Context, a *Artifact) error {
	_, err := p.db.ExecContext(ctx, `
INSERT INTO artifacts (id, job_id, name, type, size_bytes, sha256, storage, created_at, uploaded_by, workstation_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (id) DO UPDATE SET
  name=EXCLUDED.name, type=EXCLUDED.type, size_bytes=EXCLUDED.size_bytes,
  sha256=EXCLUDED.sha256, storage=EXCLUDED.storage,
  uploaded_by=EXCLUDED.uploaded_by, workstation_id=EXCLUDED.workstation_id`,
		a.ID, a.JobID, a.Name, a.Type, a.SizeBytes, a.SHA256, a.Storage, a.CreatedAt, a.UploadedBy, a.WorkstationID,
	)
	return err
}

func (p *PostgresStore) Get(ctx context.Context, id string) (*Artifact, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+artCols+` FROM artifacts WHERE id=$1`, id)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (p *PostgresStore) GetByHash(ctx context.Context, hash string) (*Artifact, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+artCols+` FROM artifacts WHERE sha256=$1 ORDER BY created_at ASC LIMIT 1`, hash)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return a, err
}

func (p *PostgresStore) ListByJob(ctx context.Context, jobID string) ([]*Artifact, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+artCols+` FROM artifacts WHERE job_id=$1 ORDER BY created_at DESC`, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectArtifacts(rows)
}

func (p *PostgresStore) List(ctx context.Context) ([]*Artifact, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT `+artCols+` FROM artifacts ORDER BY created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectArtifacts(rows)
}

func collectArtifacts(rows *sql.Rows) ([]*Artifact, error) {
	var out []*Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
