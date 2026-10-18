package taskconversation

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PostgresStore 使用 PostgreSQL 保存卡片映射与对话历史。
type PostgresStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{db: db} }

func (s *PostgresStore) CreateTask(ctx context.Context, c *Conversation, prompt string, generation int64) error {
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_conversations
		(id, employee_id, employee_name, owner_user_id, owner_open_id, chat_id, current_job_id, card_message_id, status, active_provider, active_model, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11,$12,$13)
		ON CONFLICT (id) DO NOTHING`, c.ID, c.EmployeeID, c.EmployeeName, c.OwnerUserID,
		c.OwnerOpenID, c.ChatID, c.CurrentJobID, c.CardMessageID, c.Status, c.Provider, c.Model, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_conversation_turns
		(conversation_id,job_id,turn_no,status,session_generation,created_at)
		VALUES ($1,$2,1,'CREATED',$3,$4) ON CONFLICT(job_id) DO NOTHING`, c.ID, c.CurrentJobID, generation, now)
	if err != nil {
		return err
	}
	var existingConversationID string
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id FROM task_conversation_turns WHERE job_id=$1`, c.CurrentJobID).Scan(&existingConversationID); err != nil {
		return err
	}
	if existingConversationID != c.ID {
		return ErrExists
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO task_conversation_messages
		(conversation_id,job_id,role,content,status,created_at)
		VALUES ($1,$2,'user',$3,'DONE',$4),($1,$2,'assistant','','RUNNING',$4)
		ON CONFLICT(job_id,role) DO NOTHING`, c.ID, c.CurrentJobID, prompt, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

const conversationCols = `id, employee_id, employee_name, owner_user_id, owner_open_id, chat_id,
	COALESCE(current_job_id,''), COALESCE(card_message_id,''), status, active_provider, active_model, created_at, updated_at`

func scanConversation(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.EmployeeID, &c.EmployeeName, &c.OwnerUserID, &c.OwnerOpenID, &c.ChatID,
		&c.CurrentJobID, &c.CardMessageID, &c.Status, &c.Provider, &c.Model, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (s *PostgresStore) Get(ctx context.Context, id string) (*Conversation, error) {
	return scanConversation(s.db.QueryRowContext(ctx, `SELECT `+conversationCols+` FROM task_conversations WHERE id=$1`, id))
}

func (s *PostgresStore) FindByJob(ctx context.Context, jobID string) (*Conversation, error) {
	return scanConversation(s.db.QueryRowContext(ctx, `
		SELECT c.id,c.employee_id,c.employee_name,c.owner_user_id,c.owner_open_id,c.chat_id,
			COALESCE(c.current_job_id,''),COALESCE(c.card_message_id,''),c.status,c.active_provider,c.active_model,c.created_at,c.updated_at
		FROM task_conversations c
		JOIN task_conversation_turns t ON t.conversation_id=c.id WHERE t.job_id=$1 AND c.merged_into_id IS NULL`, jobID))
}

func (s *PostgresStore) ListIDs(ctx context.Context, limit int) ([]string, error) {
	query := `SELECT id FROM task_conversations WHERE merged_into_id IS NULL ORDER BY updated_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		if limit > 500 {
			limit = 500
		}
		rows, err = s.db.QueryContext(ctx, query+` LIMIT $1`, limit)
	} else {
		rows, err = s.db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *PostgresStore) ListEmployeeIDs(ctx context.Context, employeeID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM task_conversations
		WHERE employee_id=$1 AND merged_into_id IS NULL ORDER BY updated_at DESC LIMIT $2`, employeeID, limit)
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

func (s *PostgresStore) SessionJobIDs(ctx context.Context, employeeID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.job_id FROM task_conversation_turns t
		JOIN task_conversations c ON c.id=t.conversation_id
		WHERE c.employee_id=$1 AND c.merged_into_id IS NULL ORDER BY t.created_at,t.job_id`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var jobID string
		if err := rows.Scan(&jobID); err != nil {
			return nil, err
		}
		out = append(out, jobID)
	}
	return out, rows.Err()
}

func (s *PostgresStore) SessionMessages(ctx context.Context, employeeID string, generation int64) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.conversation_id,m.job_id,t.turn_no,t.session_generation,m.role,m.content,m.status,m.created_at
		FROM task_conversation_messages m
		JOIN task_conversation_turns t ON t.job_id=m.job_id
		JOIN task_conversations c ON c.id=t.conversation_id
		WHERE c.employee_id=$1 AND c.merged_into_id IS NULL AND t.session_generation=$2
		ORDER BY t.created_at, CASE m.role WHEN 'user' THEN 0 ELSE 1 END,m.id`, employeeID, generation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ConversationID, &m.JobID, &m.TurnNo, &m.SessionGeneration, &m.Role, &m.Content, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) SetRuntime(ctx context.Context, jobID, provider, model string, generation int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE task_conversation_turns SET session_generation=$2 WHERE job_id=$1`, jobID, generation)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	res, err = tx.ExecContext(ctx, `UPDATE task_conversations c
		SET active_provider=$2, active_model=$3, updated_at=NOW()
		FROM task_conversation_turns t
		WHERE t.job_id=$1 AND c.id=t.conversation_id AND c.merged_into_id IS NULL`, jobID, provider, model)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

func (s *PostgresStore) SetCardMessageID(ctx context.Context, id, messageID string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE task_conversations SET card_message_id=$2,updated_at=NOW() WHERE id=$1`, id, messageID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *PostgresStore) SetTurnStatus(ctx context.Context, jobID, status string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE task_conversation_turns SET status=$2 WHERE job_id=$1`, jobID, status)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, err = tx.ExecContext(ctx, `UPDATE task_conversation_messages SET status=$2 WHERE job_id=$1 AND role='assistant'`, jobID, status)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE task_conversations c SET status=$2,updated_at=NOW()
		FROM task_conversation_turns t WHERE t.job_id=$1 AND c.id=t.conversation_id AND c.current_job_id=t.job_id`, jobID, status)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) AppendDelta(ctx context.Context, eventID, jobID string, sequence uint64, text string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var conversationID, status string
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id,status FROM task_conversation_turns WHERE job_id=$1 FOR UPDATE`, jobID).
		Scan(&conversationID, &status); err != nil {
		return err
	}
	if isTerminal(status) {
		return tx.Commit()
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO task_conversation_deltas(event_id,conversation_id,job_id,sequence,content)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, eventID, conversationID, jobID, sequence, text)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, err = tx.ExecContext(ctx, `
			UPDATE task_conversation_messages m
			SET content=COALESCE((SELECT string_agg(d.content,'' ORDER BY d.sequence)
				FROM task_conversation_deltas d WHERE d.job_id=$1),'')
			WHERE m.job_id=$1 AND m.role='assistant'`, jobID)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE task_conversations SET updated_at=NOW() WHERE id=$1`, conversationID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) AppendActivity(ctx context.Context, eventID, jobID string, sequence uint64, text string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var conversationID, status string
	if err := tx.QueryRowContext(ctx, `SELECT conversation_id,status FROM task_conversation_turns WHERE job_id=$1 FOR UPDATE`, jobID).
		Scan(&conversationID, &status); err != nil {
		return err
	}
	if isTerminal(status) {
		return tx.Commit()
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO task_conversation_activities(event_id,conversation_id,job_id,sequence,content)
		VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING`, eventID, conversationID, jobID, sequence, text)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n > 0 {
		_, err = tx.ExecContext(ctx, `UPDATE task_conversations SET updated_at=NOW() WHERE id=$1`, conversationID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *PostgresStore) SetAssistantFinal(ctx context.Context, jobID, content, status string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE task_conversation_messages SET content=$2,status=$3 WHERE job_id=$1 AND role='assistant'`, jobID, content, status)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	_, err = tx.ExecContext(ctx, `UPDATE task_conversations c SET updated_at=NOW() FROM task_conversation_turns t WHERE t.job_id=$1 AND c.id=t.conversation_id`, jobID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PostgresStore) Messages(ctx context.Context, conversationID string) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.conversation_id,m.job_id,t.turn_no,t.session_generation,m.role,m.content,m.status,m.created_at
		FROM task_conversation_messages m
		JOIN task_conversation_turns t ON t.job_id=m.job_id
		WHERE m.conversation_id=$1
		ORDER BY t.turn_no, CASE m.role WHEN 'user' THEN 0 ELSE 1 END`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ConversationID, &m.JobID, &m.TurnNo, &m.SessionGeneration, &m.Role, &m.Content, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Activities(ctx context.Context, conversationID, jobID string, limit int) ([]Activity, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT job_id,sequence,content,created_at FROM task_conversation_activities
		WHERE conversation_id=$1 AND job_id=$2
		ORDER BY sequence DESC LIMIT $3`, conversationID, jobID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Activity
	for rows.Next() {
		var a Activity
		if err := rows.Scan(&a.JobID, &a.Sequence, &a.Content, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}
