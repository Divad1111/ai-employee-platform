package store

import (
	"context"
	"fmt"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// PersistCommand 将 Server 命令写入 server_commands（ACK 前必须成功）。
func (s *Store) PersistCommand(ctx context.Context, cmd *aiev1.Command) error {
	if cmd == nil {
		return fmt.Errorf("空命令")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO server_commands(command_id, sequence, type, payload, status, acked, received_at)
VALUES(?, ?, ?, ?, 'RECEIVED', 0, ?)
ON CONFLICT(command_id) DO NOTHING
`, cmd.GetCommandId(), cmd.GetMeta().GetSequence(), cmd.GetType().String(), cmd.GetPayloadJson(), now)
	return err
}

// MarkAcked 标记命令已 ACK。
func (s *Store) MarkAcked(ctx context.Context, commandID string, sequence uint64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
UPDATE server_commands SET acked=1, acked_at=?, status='ACKED' WHERE command_id=? AND sequence=?
`, now, commandID, sequence)
	return err
}

// LastAckedSequence 查询最大已 ACK 序号。
func (s *Store) LastAckedSequence(ctx context.Context) (uint64, error) {
	var seq uint64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence),0) FROM server_commands WHERE acked=1`).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq, nil
}

// EnqueueOutbox 写入 outbox。
func (s *Store) EnqueueOutbox(ctx context.Context, ev *aiev1.Event) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO outbox(event_id, sequence, payload, created_at, status)
VALUES(?, ?, ?, ?, 'PENDING')
ON CONFLICT(event_id) DO NOTHING
`, ev.GetEventId(), ev.GetMeta().GetSequence(), ev.GetPayloadJson(), now)
	return err
}

// SQLJournal 将 Store 适配为 ack.Journal（需已打开 DB）。
type SQLJournal struct {
	Store *Store
}

func (j SQLJournal) PersistCommand(cmd *aiev1.Command) error {
	return j.Store.PersistCommand(context.Background(), cmd)
}
func (j SQLJournal) MarkAcked(commandID string, sequence uint64) error {
	return j.Store.MarkAcked(context.Background(), commandID, sequence)
}
func (j SQLJournal) LastAckedSequence() (uint64, error) {
	return j.Store.LastAckedSequence(context.Background())
}
