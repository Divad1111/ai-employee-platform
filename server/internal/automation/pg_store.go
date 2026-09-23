package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// PostgresStore PostgreSQL 实现。
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore 创建。
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (p *PostgresStore) Save(ctx context.Context, a *Automation) error {
	cfg := a.TriggerConfig
	if cfg == nil {
		cfg = json.RawMessage(`{}`)
	}
	_, err := p.db.ExecContext(ctx, `
INSERT INTO automations (id, name, trigger_type, enabled, employee_id, prompt, timezone, notify_chat_id, trigger_config, last_fired_at, created_by, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
ON CONFLICT (id) DO UPDATE SET
  name=EXCLUDED.name, enabled=EXCLUDED.enabled, employee_id=EXCLUDED.employee_id,
  prompt=EXCLUDED.prompt, timezone=EXCLUDED.timezone, notify_chat_id=EXCLUDED.notify_chat_id,
  trigger_config=EXCLUDED.trigger_config, last_fired_at=EXCLUDED.last_fired_at,
  updated_at=EXCLUDED.updated_at`,
		a.ID, a.Name, a.TriggerType, a.Enabled, a.EmployeeID, a.Prompt, a.Timezone, a.NotifyChatID,
		[]byte(cfg), a.LastFiredAt, a.CreatedBy, a.CreatedAt, a.UpdatedAt,
	)
	return err
}

func scanAuto(row interface{ Scan(dest ...any) error }) (*Automation, error) {
	var a Automation
	var cfg []byte
	var last sql.NullTime
	err := row.Scan(&a.ID, &a.Name, &a.TriggerType, &a.Enabled, &a.EmployeeID, &a.Prompt, &a.Timezone,
		&a.NotifyChatID, &cfg, &last, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	a.TriggerConfig = cfg
	if last.Valid {
		t := last.Time.UTC()
		a.LastFiredAt = &t
	}
	return &a, nil
}

const autoCols = `id, name, trigger_type, enabled, employee_id, prompt, timezone, notify_chat_id, trigger_config, last_fired_at, created_by, created_at, updated_at`

func (p *PostgresStore) Get(ctx context.Context, id string) (*Automation, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+autoCols+` FROM automations WHERE id=$1`, id)
	a, err := scanAuto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (p *PostgresStore) List(ctx context.Context, triggerType string) ([]*Automation, error) {
	var rows *sql.Rows
	var err error
	if triggerType == "" {
		rows, err = p.db.QueryContext(ctx, `SELECT `+autoCols+` FROM automations ORDER BY created_at DESC`)
	} else {
		rows, err = p.db.QueryContext(ctx, `SELECT `+autoCols+` FROM automations WHERE trigger_type=$1 ORDER BY created_at DESC`, triggerType)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Automation
	for rows.Next() {
		a, err := scanAuto(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *PostgresStore) Delete(ctx context.Context, id string) error {
	res, err := p.db.ExecContext(ctx, `DELETE FROM automations WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *PostgresStore) SaveCalendarItem(ctx context.Context, item *CalendarItem) error {
	_, err := p.db.ExecContext(ctx, `
INSERT INTO automation_calendar_items (id, automation_id, run_date, seq, employee_id, prompt, enabled, created_at, updated_at)
VALUES ($1,$2,$3::date,$4,$5,$6,$7,$8,$9)
ON CONFLICT (id) DO UPDATE SET
  seq=EXCLUDED.seq, employee_id=EXCLUDED.employee_id, prompt=EXCLUDED.prompt,
  enabled=EXCLUDED.enabled, updated_at=EXCLUDED.updated_at`,
		item.ID, item.AutomationID, item.RunDate, item.Seq, item.EmployeeID, item.Prompt, item.Enabled, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (p *PostgresStore) ListCalendarItems(ctx context.Context, automationID, runDate string) ([]*CalendarItem, error) {
	var rows *sql.Rows
	var err error
	if runDate == "" {
		rows, err = p.db.QueryContext(ctx, `
SELECT id, automation_id, run_date::text, seq, employee_id, prompt, enabled, created_at, updated_at
FROM automation_calendar_items WHERE automation_id=$1 ORDER BY run_date, seq`, automationID)
	} else {
		rows, err = p.db.QueryContext(ctx, `
SELECT id, automation_id, run_date::text, seq, employee_id, prompt, enabled, created_at, updated_at
FROM automation_calendar_items WHERE automation_id=$1 AND run_date=$2::date ORDER BY seq`, automationID, runDate)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*CalendarItem
	for rows.Next() {
		var it CalendarItem
		if err := rows.Scan(&it.ID, &it.AutomationID, &it.RunDate, &it.Seq, &it.EmployeeID, &it.Prompt, &it.Enabled, &it.CreatedAt, &it.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &it)
	}
	return out, rows.Err()
}

func (p *PostgresStore) GetCalendarItem(ctx context.Context, id string) (*CalendarItem, error) {
	row := p.db.QueryRowContext(ctx, `
SELECT id, automation_id, run_date::text, seq, employee_id, prompt, enabled, created_at, updated_at
FROM automation_calendar_items WHERE id=$1`, id)
	var it CalendarItem
	err := row.Scan(&it.ID, &it.AutomationID, &it.RunDate, &it.Seq, &it.EmployeeID, &it.Prompt, &it.Enabled, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &it, err
}

func (p *PostgresStore) DeleteCalendarItemsByDate(ctx context.Context, automationID, runDate string) error {
	_, err := p.db.ExecContext(ctx, `DELETE FROM automation_calendar_items WHERE automation_id=$1 AND run_date=$2::date`, automationID, runDate)
	return err
}

func (p *PostgresStore) ReplaceCalendarItems(ctx context.Context, automationID, runDate string, items []*CalendarItem) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM automation_calendar_items WHERE automation_id=$1 AND run_date=$2::date`, automationID, runDate); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO automation_calendar_items (id, automation_id, run_date, seq, employee_id, prompt, enabled, created_at, updated_at)
VALUES ($1,$2,$3::date,$4,$5,$6,$7,$8,$9)`,
			item.ID, item.AutomationID, item.RunDate, item.Seq, item.EmployeeID, item.Prompt, item.Enabled, item.CreatedAt, item.UpdatedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (p *PostgresStore) SaveRun(ctx context.Context, r *Run) error {
	payload := r.Payload
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	_, err := p.db.ExecContext(ctx, `
INSERT INTO automation_runs (id, automation_id, calendar_item_id, job_id, status, trigger_source, idempotency_key, error, payload, created_at, finished_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
ON CONFLICT (id) DO UPDATE SET
  calendar_item_id=EXCLUDED.calendar_item_id, job_id=EXCLUDED.job_id, status=EXCLUDED.status,
  error=EXCLUDED.error, payload=EXCLUDED.payload, finished_at=EXCLUDED.finished_at`,
		r.ID, r.AutomationID, nullStr(r.CalendarItemID), nullStr(r.JobID), r.Status, r.TriggerSource,
		r.IdempotencyKey, r.Error, []byte(payload), r.CreatedAt, r.FinishedAt,
	)
	return err
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func scanRun(row interface{ Scan(dest ...any) error }) (*Run, error) {
	var r Run
	var calID, jobID sql.NullString
	var payload []byte
	var fin sql.NullTime
	err := row.Scan(&r.ID, &r.AutomationID, &calID, &jobID, &r.Status, &r.TriggerSource, &r.IdempotencyKey, &r.Error, &payload, &r.CreatedAt, &fin)
	if err != nil {
		return nil, err
	}
	r.CalendarItemID = calID.String
	r.JobID = jobID.String
	r.Payload = payload
	if fin.Valid {
		t := fin.Time.UTC()
		r.FinishedAt = &t
	}
	return &r, nil
}

const runCols = `id, automation_id, calendar_item_id, job_id, status, trigger_source, idempotency_key, error, payload, created_at, finished_at`

func (p *PostgresStore) GetRun(ctx context.Context, id string) (*Run, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM automation_runs WHERE id=$1`, id)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (p *PostgresStore) GetRunByIdempotency(ctx context.Context, key string) (*Run, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM automation_runs WHERE idempotency_key=$1`, key)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (p *PostgresStore) GetRunByJobID(ctx context.Context, jobID string) (*Run, error) {
	row := p.db.QueryRowContext(ctx, `SELECT `+runCols+` FROM automation_runs WHERE job_id=$1`, jobID)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func (p *PostgresStore) ListRuns(ctx context.Context, automationID string, limit int) ([]*Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.db.QueryContext(ctx, `SELECT `+runCols+` FROM automation_runs WHERE automation_id=$1 ORDER BY created_at DESC LIMIT $2`, automationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresStore) HasActiveOrSuccessForItem(ctx context.Context, calendarItemID string) (bool, error) {
	var n int
	err := p.db.QueryRowContext(ctx, `
SELECT COUNT(1) FROM automation_runs
WHERE calendar_item_id=$1 AND status IN ('triggered','job_created','success')`, calendarItemID).Scan(&n)
	return n > 0, err
}

func (p *PostgresStore) NextCalendarItem(ctx context.Context, automationID, runDate string, afterSeq int) (*CalendarItem, error) {
	row := p.db.QueryRowContext(ctx, `
SELECT id, automation_id, run_date::text, seq, employee_id, prompt, enabled, created_at, updated_at
FROM automation_calendar_items
WHERE automation_id=$1 AND run_date=$2::date AND enabled=TRUE AND seq>$3
ORDER BY seq ASC LIMIT 1`, automationID, runDate, afterSeq)
	var it CalendarItem
	err := row.Scan(&it.ID, &it.AutomationID, &it.RunDate, &it.Seq, &it.EmployeeID, &it.Prompt, &it.Enabled, &it.CreatedAt, &it.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &it, err
}

func (p *PostgresStore) GetByWebhookPathToken(ctx context.Context, pathToken string) (*Automation, error) {
	row := p.db.QueryRowContext(ctx, `
SELECT `+autoCols+` FROM automations
WHERE trigger_type='webhook' AND trigger_config->>'path_token'=$1
LIMIT 1`, pathToken)
	a, err := scanAuto(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

var _ Store = (*PostgresStore)(nil)