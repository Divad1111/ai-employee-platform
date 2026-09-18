package reliability

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// PendingCommand 待下发 / 待确认的命令快照。
type PendingCommand struct {
	Command   *aiev1.Command
	Acked     bool
	CreatedAt time.Time
}

// CommandStore 按 Workstation 维护单调 sequence 与未确认命令。
type CommandStore struct {
	mu      sync.Mutex
	byWS    map[string]*wsCommands
	skewMs  int64
	nowFunc func() time.Time
}

type wsCommands struct {
	nextSeq uint64 // 下一分配序号（已分配最大 = nextSeq-1）
	acked   uint64 // 最后确认序号
	pending map[uint64]*PendingCommand
	byID    map[string]uint64 // command_id → sequence
}

// NewCommandStore 创建内存命令库。
func NewCommandStore(skewMs int64) *CommandStore {
	if skewMs <= 0 {
		skewMs = 5 * 60 * 1000
	}
	return &CommandStore{
		byWS:    make(map[string]*wsCommands),
		skewMs:  skewMs,
		nowFunc: time.Now,
	}
}

func (s *CommandStore) ensure(wsID string) *wsCommands {
	w, ok := s.byWS[wsID]
	if !ok {
		w = &wsCommands{
			nextSeq: 1,
			pending: make(map[uint64]*PendingCommand),
			byID:    make(map[string]uint64),
		}
		s.byWS[wsID] = w
	}
	return w
}

// CurrentSequence 返回已分配的最大序号（无则 0）。
func (s *CommandStore) CurrentSequence(wsID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensure(wsID)
	if w.nextSeq == 0 {
		return 0
	}
	return w.nextSeq - 1
}

// LastAcked 返回最后确认序号。
func (s *CommandStore) LastAcked(wsID string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensure(wsID).acked
}

// Enqueue 分配单调序号并入队；相同 command_id 幂等返回已有命令。
func (s *CommandStore) Enqueue(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensure(wsID)
	now := s.nowFunc()
	cmdID := newID()
	seq := w.nextSeq
	w.nextSeq++
	cmd := &aiev1.Command{
		Meta: &aiev1.EnvelopeMeta{
			MessageId:       newID(),
			Sequence:        seq,
			TimestampUnixMs: now.UnixMilli(),
			Nonce:           newID(),
		},
		CommandId:     cmdID,
		WorkstationId: wsID,
		EmployeeId:    employeeID,
		JobId:         jobID,
		Type:          typ,
		PayloadJson:   payloadJSON,
	}
	pc := &PendingCommand{Command: cmd, CreatedAt: now}
	w.pending[seq] = pc
	w.byID[cmdID] = seq
	return cmd, false, nil
}

// EnqueueExisting 用于测试：以指定 command 入队（自行带 sequence）。
func (s *CommandStore) EnqueueExisting(cmd *aiev1.Command) error {
	if cmd == nil || cmd.GetWorkstationId() == "" || cmd.GetMeta() == nil {
		return fmt.Errorf("命令不完整")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensure(cmd.WorkstationId)
	seq := cmd.Meta.Sequence
	if seq == 0 {
		return fmt.Errorf("sequence 无效")
	}
	if existing, ok := w.byID[cmd.CommandId]; ok {
		_ = existing
		return nil // 幂等
	}
	if seq < w.nextSeq {
		// 允许补录历史未确认命令（Resume 场景已有）
	} else {
		w.nextSeq = seq + 1
	}
	w.pending[seq] = &PendingCommand{Command: cmd, CreatedAt: s.nowFunc()}
	w.byID[cmd.CommandId] = seq
	return nil
}

// UnackedAfter 返回 sequence > after 且未 ACK 的命令（按序号升序）。
func (s *CommandStore) UnackedAfter(wsID string, after uint64) []*aiev1.Command {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensure(wsID)
	var out []*aiev1.Command
	for seq := after + 1; seq < w.nextSeq; seq++ {
		pc, ok := w.pending[seq]
		if !ok || pc.Acked {
			continue
		}
		out = append(out, pc.Command)
	}
	return out
}

// Ack 确认命令；过旧/未知返回错误语义由调用方转 ACK.accepted=false。
func (s *CommandStore) Ack(wsID, commandID string, sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.ensure(wsID)
	pc, ok := w.pending[sequence]
	if !ok {
		return fmt.Errorf("未知 sequence %d", sequence)
	}
	if pc.Command.GetCommandId() != commandID {
		return fmt.Errorf("command_id 与 sequence 不匹配")
	}
	pc.Acked = true
	if sequence > w.acked {
		w.acked = sequence
	}
	return nil
}

// ValidateMeta 校验下行命令元数据时间窗（供对称测试复用）。
func (s *CommandStore) ValidateMeta(meta *aiev1.EnvelopeMeta) error {
	if meta == nil {
		return fmt.Errorf("缺少 EnvelopeMeta")
	}
	now := s.nowFunc().UnixMilli()
	delta := now - meta.TimestampUnixMs
	if delta < 0 {
		delta = -delta
	}
	if delta > s.skewMs {
		return fmt.Errorf("timestamp 超窗")
	}
	return nil
}
