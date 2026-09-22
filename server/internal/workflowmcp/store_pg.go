package workflowmcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PostgresStore 基于 PostgreSQL 的 Store 实现。
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建 PG 存储。
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) ListWorkflows(ctx context.Context) ([]*Workflow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, version, description, when_to_use, inputs, skill_refs, knowledge_refs,
		       steps, approval, extra, status, created_at, updated_at
		FROM workflows ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Workflow
	for rows.Next() {
		w, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, version, description, when_to_use, inputs, skill_refs, knowledge_refs,
		       steps, approval, extra, status, created_at, updated_at
		FROM workflows WHERE id = $1`, id)
	w, err := scanWorkflow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return w, err
}

func (s *PostgresStore) UpsertWorkflow(ctx context.Context, w *Workflow) error {
	now := time.Now().UTC()
	if w.CreatedAt.IsZero() {
		w.CreatedAt = now
	}
	w.UpdatedAt = now
	whenToUse, _ := json.Marshal(w.WhenToUse)
	inputs, _ := json.Marshal(w.Inputs)
	skills, _ := json.Marshal(w.SkillRefs)
	knowledge, _ := json.Marshal(w.KnowledgeRefs)
	steps, _ := json.Marshal(w.Steps)
	approval, _ := json.Marshal(w.Approval)
	extra, _ := json.Marshal(w.Extra)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO workflows (
			id, name, version, description, when_to_use, inputs, skill_refs, knowledge_refs,
			steps, approval, extra, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (id) DO UPDATE SET
			name=EXCLUDED.name, version=EXCLUDED.version, description=EXCLUDED.description,
			when_to_use=EXCLUDED.when_to_use, inputs=EXCLUDED.inputs,
			skill_refs=EXCLUDED.skill_refs, knowledge_refs=EXCLUDED.knowledge_refs,
			steps=EXCLUDED.steps, approval=EXCLUDED.approval, extra=EXCLUDED.extra,
			status=EXCLUDED.status, updated_at=EXCLUDED.updated_at`,
		w.ID, w.Name, w.Version, w.Description, whenToUse, inputs, skills, knowledge,
		steps, approval, extra, w.Status, w.CreatedAt, w.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) DeleteWorkflow(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM workflows WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListSkills(ctx context.Context) ([]*SkillPackage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, cursor_name, name, version, description, body, metadata,
		       disable_model_invocation, content_hash, created_at, updated_at
		FROM skill_packages ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*SkillPackage
	for rows.Next() {
		sp, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetSkill(ctx context.Context, idOrCursor string) (*SkillPackage, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, cursor_name, name, version, description, body, metadata,
		       disable_model_invocation, content_hash, created_at, updated_at
		FROM skill_packages WHERE id = $1 OR cursor_name = $1`, idOrCursor)
	sp, err := scanSkill(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sp, err
}

func (s *PostgresStore) GetSkillFiles(ctx context.Context, skillID string) ([]SkillFile, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT path, content, sha256, size_bytes FROM skill_package_files
		WHERE skill_id = $1 ORDER BY path`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SkillFile
	for rows.Next() {
		var f SkillFile
		if err := rows.Scan(&f.Path, &f.Content, &f.SHA256, &f.Size); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (s *PostgresStore) UpsertSkill(ctx context.Context, sp *SkillPackage, files []SkillFile) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if sp.CreatedAt.IsZero() {
		sp.CreatedAt = now
	}
	sp.UpdatedAt = now
	meta, _ := json.Marshal(sp.Metadata)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO skill_packages (
			id, cursor_name, name, version, description, body, metadata,
			disable_model_invocation, content_hash, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			cursor_name=EXCLUDED.cursor_name, name=EXCLUDED.name, version=EXCLUDED.version,
			description=EXCLUDED.description, body=EXCLUDED.body, metadata=EXCLUDED.metadata,
			disable_model_invocation=EXCLUDED.disable_model_invocation,
			content_hash=EXCLUDED.content_hash, updated_at=EXCLUDED.updated_at`,
		sp.ID, sp.CursorName, sp.Name, sp.Version, sp.Description, sp.Body, meta,
		sp.DisableModelInvocation, sp.ContentHash, sp.CreatedAt, sp.UpdatedAt,
	)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM skill_package_files WHERE skill_id = $1`, sp.ID); err != nil {
		return err
	}
	for _, f := range files {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO skill_package_files (skill_id, path, content, sha256, size_bytes)
			VALUES ($1,$2,$3,$4,$5)`, sp.ID, f.Path, f.Content, f.SHA256, f.Size); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) DeleteSkill(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM skill_packages WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ListKnowledge(ctx context.Context, namespace string) ([]*KnowledgeDoc, error) {
	var rows *sql.Rows
	var err error
	if namespace == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, path, namespace, title, source, content, metadata, aliases, version, created_at, updated_at
			FROM knowledge_docs ORDER BY id`)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, path, namespace, title, source, content, metadata, aliases, version, created_at, updated_at
			FROM knowledge_docs WHERE namespace = $1 ORDER BY id`, namespace)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*KnowledgeDoc
	for rows.Next() {
		d, err := scanKnowledge(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *PostgresStore) GetKnowledge(ctx context.Context, idOrAlias string) (*KnowledgeDoc, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, path, namespace, title, source, content, metadata, aliases, version, created_at, updated_at
		FROM knowledge_docs
		WHERE id = $1 OR path = $1 OR aliases ? $1`, idOrAlias)
	d, err := scanKnowledge(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func (s *PostgresStore) UpsertKnowledge(ctx context.Context, doc *KnowledgeDoc) error {
	now := time.Now().UTC()
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = now
	}
	doc.UpdatedAt = now
	meta, _ := json.Marshal(doc.Metadata)
	aliases, _ := json.Marshal(doc.Aliases)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO knowledge_docs (
			id, path, namespace, title, source, content, metadata, aliases, version, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO UPDATE SET
			path=EXCLUDED.path, namespace=EXCLUDED.namespace, title=EXCLUDED.title,
			source=EXCLUDED.source, content=EXCLUDED.content, metadata=EXCLUDED.metadata,
			aliases=EXCLUDED.aliases, version=EXCLUDED.version, updated_at=EXCLUDED.updated_at`,
		doc.ID, doc.Path, doc.Namespace, doc.Title, doc.Source, doc.Content,
		meta, aliases, doc.Version, doc.CreatedAt, doc.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) DeleteKnowledge(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_docs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) ReplaceChunks(ctx context.Context, docID string, chunks []KnowledgeChunk) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM knowledge_chunks WHERE doc_id = $1`, docID); err != nil {
		return err
	}
	for _, c := range chunks {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO knowledge_chunks (doc_id, chunk_index, heading, content, tokens, tsv)
			VALUES ($1,$2,$3,$4,$5, to_tsvector('simple', $5))`,
			c.DocID, c.ChunkIndex, c.Heading, c.Content, c.Tokens)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) SearchKnowledge(ctx context.Context, query, namespace string, limit int) ([]KnowledgeHit, error) {
	if limit <= 0 {
		limit = 10
	}
	tsq := TokenizeToTSQuery(query)
	if tsq == "" {
		return nil, nil
	}
	var rows *sql.Rows
	var err error
	if namespace == "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT d.id, d.title, d.source, d.path, d.metadata, c.content,
			       ts_rank_cd(c.tsv, to_tsquery('simple', $1)) AS rank
			FROM knowledge_chunks c
			JOIN knowledge_docs d ON d.id = c.doc_id
			WHERE c.tsv @@ to_tsquery('simple', $1)
			ORDER BY rank DESC
			LIMIT $2`, tsq, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT d.id, d.title, d.source, d.path, d.metadata, c.content,
			       ts_rank_cd(c.tsv, to_tsquery('simple', $1)) AS rank
			FROM knowledge_chunks c
			JOIN knowledge_docs d ON d.id = c.doc_id
			WHERE c.tsv @@ to_tsquery('simple', $1) AND d.namespace = $2
			ORDER BY rank DESC
			LIMIT $3`, tsq, namespace, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnowledgeHit
	seen := map[string]bool{}
	for rows.Next() {
		var h KnowledgeHit
		var meta []byte
		var rank float64
		if err := rows.Scan(&h.ID, &h.Title, &h.Source, &h.Path, &meta, &h.Content, &rank); err != nil {
			return nil, err
		}
		if seen[h.ID] {
			continue
		}
		seen[h.ID] = true
		_ = json.Unmarshal(meta, &h.Metadata)
		h.Score = rank
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListAllDocIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM knowledge_docs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *PostgresStore) ListGrantsByEmployee(ctx context.Context, employeeID string) ([]EmployeeGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT employee_id, workflow_id, granted_by, granted_at
		FROM employee_workflows WHERE employee_id = $1`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGrants(rows)
}

func (s *PostgresStore) ListEmployeesByWorkflow(ctx context.Context, workflowID string) ([]EmployeeGrant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT employee_id, workflow_id, granted_by, granted_at
		FROM employee_workflows WHERE workflow_id = $1`, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanGrants(rows)
}

func (s *PostgresStore) GrantWorkflow(ctx context.Context, g EmployeeGrant) error {
	if g.GrantedAt.IsZero() {
		g.GrantedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO employee_workflows (employee_id, workflow_id, granted_by, granted_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (employee_id, workflow_id) DO UPDATE SET
			granted_by=EXCLUDED.granted_by, granted_at=EXCLUDED.granted_at`,
		g.EmployeeID, g.WorkflowID, g.GrantedBy, g.GrantedAt)
	return err
}

func (s *PostgresStore) RevokeWorkflow(ctx context.Context, employeeID, workflowID string) error {
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM employee_workflows WHERE employee_id = $1 AND workflow_id = $2`,
		employeeID, workflowID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- scanners ---

type scannable interface {
	Scan(dest ...any) error
}

func scanWorkflow(row scannable) (*Workflow, error) {
	var w Workflow
	var whenToUse, inputs, skills, knowledge, steps, approval, extra []byte
	err := row.Scan(&w.ID, &w.Name, &w.Version, &w.Description,
		&whenToUse, &inputs, &skills, &knowledge, &steps, &approval, &extra,
		&w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(whenToUse, &w.WhenToUse)
	_ = json.Unmarshal(inputs, &w.Inputs)
	_ = json.Unmarshal(skills, &w.SkillRefs)
	_ = json.Unmarshal(knowledge, &w.KnowledgeRefs)
	_ = json.Unmarshal(steps, &w.Steps)
	_ = json.Unmarshal(approval, &w.Approval)
	_ = json.Unmarshal(extra, &w.Extra)
	if w.Extra == nil {
		w.Extra = map[string]any{}
	}
	return &w, nil
}

func scanSkill(row scannable) (*SkillPackage, error) {
	var sp SkillPackage
	var meta []byte
	err := row.Scan(&sp.ID, &sp.CursorName, &sp.Name, &sp.Version, &sp.Description, &sp.Body,
		&meta, &sp.DisableModelInvocation, &sp.ContentHash, &sp.CreatedAt, &sp.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(meta, &sp.Metadata)
	if sp.Metadata == nil {
		sp.Metadata = map[string]any{}
	}
	return &sp, nil
}

func scanKnowledge(row scannable) (*KnowledgeDoc, error) {
	var d KnowledgeDoc
	var meta, aliases []byte
	err := row.Scan(&d.ID, &d.Path, &d.Namespace, &d.Title, &d.Source, &d.Content,
		&meta, &aliases, &d.Version, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(meta, &d.Metadata)
	_ = json.Unmarshal(aliases, &d.Aliases)
	if d.Metadata == nil {
		d.Metadata = map[string]any{}
	}
	return &d, nil
}

func scanGrants(rows *sql.Rows) ([]EmployeeGrant, error) {
	var out []EmployeeGrant
	for rows.Next() {
		var g EmployeeGrant
		if err := rows.Scan(&g.EmployeeID, &g.WorkflowID, &g.GrantedBy, &g.GrantedAt); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

var _ Store = (*PostgresStore)(nil)
