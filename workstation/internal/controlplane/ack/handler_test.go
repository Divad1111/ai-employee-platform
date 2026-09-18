package ack_test

import (
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/controlplane/ack"
	"github.com/ai-employee-platform/workstation/internal/controlplane/sequence"
)

func TestACKOnlyAfterCommit(t *testing.T) {
	h := &ack.Handler{Journal: ack.FailJournal{}, Validator: sequence.NewValidator(0)}
	cmd := &aiev1.Command{
		CommandId: "c1",
		Meta: &aiev1.EnvelopeMeta{
			MessageId: "m1", Sequence: 1,
			TimestampUnixMs: time.Now().UnixMilli(),
		},
	}
	ackMsg, should, err := h.Process(cmd)
	if err == nil || should || ackMsg != nil {
		t.Fatalf("COMMIT 失败不应 ACK: ack=%v should=%v err=%v", ackMsg, should, err)
	}
}

func TestACKAfterPersist(t *testing.T) {
	j := ack.NewMemoryJournal()
	h := &ack.Handler{Journal: j, Validator: sequence.NewValidator(0)}
	cmd := &aiev1.Command{
		CommandId: "c1",
		Meta: &aiev1.EnvelopeMeta{
			MessageId: "m1", Sequence: 1,
			TimestampUnixMs: time.Now().UnixMilli(),
		},
	}
	ackMsg, should, err := h.Process(cmd)
	if err != nil || !should || ackMsg == nil || !ackMsg.Accepted {
		t.Fatalf("应 ACK: %v %v %v", ackMsg, should, err)
	}
	seq, _ := j.LastAckedSequence()
	if seq != 1 {
		t.Fatalf("lastAck=%d", seq)
	}

	// 崩溃恢复后重投相同 command_id → 幂等 ACK
	ack2, should2, err2 := h.Process(cmd)
	if err2 != nil || !should2 || !ack2.Accepted {
		t.Fatalf("幂等重投失败")
	}
}
