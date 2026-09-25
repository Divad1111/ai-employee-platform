package sequence_test

import (
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/controlplane/sequence"
)

func cmd(id string, seq uint64, skewMs int64) *aiev1.Command {
	return &aiev1.Command{
		CommandId: id,
		Meta: &aiev1.EnvelopeMeta{
			MessageId:       "m-" + id,
			Sequence:        seq,
			TimestampUnixMs: time.Now().UnixMilli() + skewMs,
			Nonce:           "n",
		},
	}
}

func TestStrictSequenceAndIdempotent(t *testing.T) {
	v := sequence.NewValidator(0)
	c1 := cmd("c1", 1, 0)
	if r := v.Check(c1); !r.Accept {
		t.Fatalf("seq1: %s", r.Error)
	}
	v.Commit(c1)

	// 重复 command_id 幂等
	if r := v.Check(c1); !r.Accept || !r.Duplicate {
		t.Fatalf("idempotent: %+v", r)
	}

	// 旧序号拒绝
	if r := v.Check(cmd("c0", 1, 0)); r.Accept {
		t.Fatal("应拒绝 sequence<=last")
	}

	// 乱序拒绝（跳号）
	if r := v.Check(cmd("c3", 3, 0)); r.Accept {
		t.Fatal("应拒绝乱序")
	}

	c2 := cmd("c2", 2, 0)
	if r := v.Check(c2); !r.Accept {
		t.Fatalf("seq2: %s", r.Error)
	}
	v.Commit(c2)
	if v.LastSequence() != 2 {
		t.Fatalf("last=%d", v.LastSequence())
	}
}

func TestSequenceRewindAfterServerRestart(t *testing.T) {
	v := sequence.NewValidator(0)
	v.SetLastSequence(40)
	c := cmd("new-epoch", 1, 0)
	if r := v.Check(c); !r.Accept {
		t.Fatalf("序号回绕应接受: %s", r.Error)
	}
	v.Commit(c)
	if v.LastSequence() != 1 {
		t.Fatalf("last=%d", v.LastSequence())
	}
}

func TestTimestampWindow(t *testing.T) {
	v := sequence.NewValidator(1000) // 1s
	far := cmd("cx", 1, 60_000)
	if r := v.Check(far); r.Accept {
		t.Fatal("超窗应拒绝")
	}
}
