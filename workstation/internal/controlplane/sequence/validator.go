package sequence

import (
	"fmt"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// 乱序策略：严格递增（必须 last+1）；不缓冲。旧序号拒绝；相同 command_id 幂等。

// Validator 校验 Server→Worker 命令序号与时间窗。
type Validator struct {
	mu      sync.Mutex
	lastSeq uint64
	seenID  map[string]uint64 // command_id → sequence
	seenMsg map[string]struct{}
	skewMs  int64
	nowFunc func() time.Time
}

// NewValidator 创建校验器。skewMs<=0 时默认 ±5 分钟。
func NewValidator(skewMs int64) *Validator {
	if skewMs <= 0 {
		skewMs = 5 * 60 * 1000
	}
	return &Validator{
		seenID:  make(map[string]uint64),
		seenMsg: make(map[string]struct{}),
		skewMs:  skewMs,
		nowFunc: time.Now,
	}
}

// LastSequence 最后接受的序号。
func (v *Validator) LastSequence() uint64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.lastSeq
}

// SetLastSequence 从本地恢复（崩溃恢复）。
func (v *Validator) SetLastSequence(seq uint64) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.lastSeq = seq
}

// Result 校验结果。
type Result struct {
	Accept    bool
	Duplicate bool // 相同 command_id / message_id
	Error     string
}

// Check 校验命令；不修改状态（Accept 才推进）。
func (v *Validator) Check(cmd *aiev1.Command) Result {
	if cmd == nil || cmd.GetCommandId() == "" || cmd.GetMeta() == nil {
		return Result{Error: "命令不完整"}
	}
	meta := cmd.GetMeta()
	now := v.nowFunc().UnixMilli()
	delta := now - meta.TimestampUnixMs
	if delta < 0 {
		delta = -delta
	}
	if delta > v.skewMs {
		return Result{Error: "timestamp 超窗"}
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if seq, ok := v.seenID[cmd.CommandId]; ok {
		_ = seq
		return Result{Accept: true, Duplicate: true}
	}
	if meta.MessageId != "" {
		if _, ok := v.seenMsg[meta.MessageId]; ok {
			return Result{Accept: true, Duplicate: true}
		}
	}
	seq := meta.Sequence
	if seq == 0 {
		return Result{Error: "sequence 无效"}
	}
	// 控制平面重启后序号从 1 重新计。工作站仍记着旧序号时，收到更小的序号视为新纪元。
	if v.lastSeq > 0 && seq < v.lastSeq {
		v.lastSeq = seq - 1
	}
	if seq <= v.lastSeq {
		return Result{Error: fmt.Sprintf("sequence <= last_sequence (%d <= %d)", seq, v.lastSeq)}
	}
	if v.lastSeq > 0 && seq != v.lastSeq+1 {
		return Result{Error: fmt.Sprintf("乱序拒绝: want %d got %d", v.lastSeq+1, seq)}
	}
	return Result{Accept: true}
}

// Commit 在本地持久化成功后推进序号。
func (v *Validator) Commit(cmd *aiev1.Command) {
	v.mu.Lock()
	defer v.mu.Unlock()
	meta := cmd.GetMeta()
	v.seenID[cmd.CommandId] = meta.Sequence
	if meta.MessageId != "" {
		v.seenMsg[meta.MessageId] = struct{}{}
	}
	if meta.Sequence > v.lastSeq {
		v.lastSeq = meta.Sequence
	}
}
