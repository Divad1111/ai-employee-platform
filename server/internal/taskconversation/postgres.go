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

func (s *PostgresStore) Create(ctx context.Context, c *Conversation) error {
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = c.CreatedAt
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_conversations
		(id, employee_id, employee_name, owner_user_id, owner_open_id, chat_id, current_job_id, card_message_id, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULLIF($7,''),$8,$9,$10,$11)
		ON CONFLICT (id) DO NOTHING`, c.ID, c.EmployeeID, c.EmployeeName, c.OwnerUserID,
		c.OwnerOpenID, c.ChatID, c.CurrentJobID, c.CardMessageID, c.Status, c.CreatedAt, c.UpdatedAt)
	return err
}

const conversationCols = `id, employee_id, employee_name, owner_user_id, owner_open_id, chat_id,
	COALESCE(current_job_id,''), COALESCE(card_message_id,''), status, created_at, updated_at`

func scanConversation(row interface{ Scan(...any) error }) (*Conversation, error) {
	var c Conversation
	err := row.Scan(&c.ID, &c.EmployeeID, &c.EmployeeName, &c.OwnerUserID, &c.OwnerOpenID, &c.ChatID,
		&c.CurrentJobID, &c.CardMessageID, &c.Status, &c.CreatedAt, &c.UpdatedAt)
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
			COALESCE(c.current_job_id,''),COALESCE(c.card_message_id,''),c.status,c.created_at,c.updated_at
		FROM task_conversations c
		JOIN task_conversation_turns t ON t.conversation_id=c.id WHERE t.job_id=$1`, jobID))
}

func (s *PostgresStore) ListIDs(ctx context.Context, limit int) ([]string, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM task_conversations ORDER BY updated_at DESC LIMIT $1`, limit)
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

func (s *PostgresStore) AttachTurn(ctx context.Context, conversationID, jobID, prompt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_conversation_turns(conversation_id,job_id,turn_no,status)
		SELECT $1,$2,COALESCE(MAX(turn_no),0)+1,'CREATED'
		FROM task_conversation_turns WHERE conversation_id=$1
		ON CONFLICT(job_id) DO NOTHING`, conversationID, jobID)
	if err != nil {
		return err
	}
	var turnNo int
	if err := tx.QueryRowContext(ctx, `SELECT turn_no FROM task_conversation_turns WHERE job_id=$1`, jobID).Scan(&turnNo); err != nil {
		return err
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_conversation_messages(conversation_id,job_id,role,content,status,created_at)
		VALUES ($1,$2,'user',$3,'DONE',$4) ON CONFLICT(job_id,role) DO NOTHING`, conversationID, jobID, prompt, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO task_conversation_messages(conversation_id,job_id,role,content,status,created_at)
		VALUES ($1,$2,'assistant','','RUNNING',$3) ON CONFLICT(job_id,role) DO NOTHING`, conversationID, jobID, now)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE task_conversations SET current_job_id=$2,status='CREATED',updated_at=$3 WHERE id=$1`, conversationID, jobID, now)
	if err != nil {
		return err
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
		SELECT m.conversation_id,m.job_id,t.turn_no,m.role,m.content,m.status,m.created_at
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
		if err := rows.Scan(&m.ConversationID, &m.JobID, &m.TurnNo, &m.Role, &m.Content, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *PostgresStore) Activities(ctx context.Context, conversationID, jobID string, limit int) ([]Activity, error) {
	if limit <= 0 || limit > 20 {
		limit = 4
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
