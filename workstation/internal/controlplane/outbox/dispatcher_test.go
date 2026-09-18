package outbox_test

import (
	"context"
	"errors"
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/controlplane/outbox"
)

func TestOutboxSurvivesDisconnect(t *testing.T) {
	st := outbox.NewMemoryStore()
	ev := &aiev1.Event{
		EventId: "e1",
		Meta:    &aiev1.EnvelopeMeta{Sequence: 1, TimestampUnixMs: time.Now().UnixMilli()},
	}
	d := &outbox.Dispatcher{Store: st}
	if err := d.Enqueue(ev); err != nil {
		t.Fatal(err)
	}
	if st.PendingCount() != 1 {
		t.Fatal("断网应保留 PENDING")
	}

	fail := true
	d.Send = func(ctx context.Context, e *aiev1.Event) error {
		if fail {
			return errors.New("断网")
		}
		return nil
	}
	_ = d.FlushOnce(context.Background())
	if st.PendingCount() != 1 {
		t.Fatal("发送失败仍应 PENDING")
	}
	fail = false
	if err := d.FlushOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if st.PendingCount() != 0 {
		t.Fatal("恢复后应标记 SENT")
	}
}
