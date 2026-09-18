package ack

import (
	"fmt"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/controlplane/sequence"
)

// Journal 本地命令持久化（SQLite 或内存）。
// Persist 必须在真正 COMMIT 成功后返回 nil；失败则不得 ACK。
type Journal interface {
	// PersistCommand 持久化命令；重复 command_id 幂等成功。
	PersistCommand(cmd *aiev1.Command) error
	// MarkAcked 标记已 ACK（可选，用于崩溃恢复查询）。
	MarkAcked(commandID string, sequence uint64) error
	// LastAckedSequence 最后已 ACK 序号。
	LastAckedSequence() (uint64, error)
}

// Handler 实现「COMMIT 后才 ACK」。
type Handler struct {
	Journal   Journal
	Validator *sequence.Validator
}

// Process 校验 → 持久化 → 返回是否应发送 ACK。
// 若 Persist 失败，返回 shouldACK=false。
func (h *Handler) Process(cmd *aiev1.Command) (ack *aiev1.CommandAck, shouldACK bool, err error) {
	if h.Journal == nil || h.Validator == nil {
		return nil, false, fmt.Errorf("Handler 未初始化")
	}
	res := h.Validator.Check(cmd)
	if !res.Accept {
		return &aiev1.CommandAck{
			CommandId:    cmd.GetCommandId(),
			Sequence:     cmd.GetMeta().GetSequence(),
			Accepted:     false,
			ErrorMessage: res.Error,
		}, true, nil // 拒绝也回 ACK(accepted=false)，避免 Server 死等
	}

	if err := h.Journal.PersistCommand(cmd); err != nil {
		// COMMIT 失败：不发送 ACK
		return nil, false, err
	}
	h.Validator.Commit(cmd)
	if err := h.Journal.MarkAcked(cmd.GetCommandId(), cmd.GetMeta().GetSequence()); err != nil {
		// 已 COMMIT 但标记失败：仍应 ACK（数据已在库）
		_ = err
	}
	return &aiev1.CommandAck{
		CommandId: cmd.GetCommandId(),
		Sequence:  cmd.GetMeta().GetSequence(),
		Accepted:  true,
	}, true, nil
}

// MemoryJournal 测试与无 SQLite 驱动时的内存实现。
type MemoryJournal struct {
	cmds   map[string]*aiev1.Command
	acked  map[string]bool
	lastAck uint64
}

// NewMemoryJournal 创建内存 Journal。
func NewMemoryJournal() *MemoryJournal {
	return &MemoryJournal{
		cmds:  make(map[string]*aiev1.Command),
		acked: make(map[string]bool),
	}
}

func (m *MemoryJournal) PersistCommand(cmd *aiev1.Command) error {
	if cmd == nil || cmd.CommandId == "" {
		return fmt.Errorf("空命令")
	}
	if _, ok := m.cmds[cmd.CommandId]; ok {
		return nil
	}
	m.cmds[cmd.CommandId] = cmd
	return nil
}

func (m *MemoryJournal) MarkAcked(commandID string, sequence uint64) error {
	m.acked[commandID] = true
	if sequence > m.lastAck {
		m.lastAck = sequence
	}
	return nil
}

func (m *MemoryJournal) LastAckedSequence() (uint64, error) {
	return m.lastAck, nil
}

// FailJournal 用于测试：Persist 始终失败。
type FailJournal struct{}

func (FailJournal) PersistCommand(*aiev1.Command) error { return fmt.Errorf("COMMIT 失败") }
func (FailJournal) MarkAcked(string, uint64) error       { return nil }
func (FailJournal) LastAckedSequence() (uint64, error)   { return 0, nil }

// NowMs 便于测试注入时间。
func NowMs() int64 { return time.Now().UnixMilli() }
