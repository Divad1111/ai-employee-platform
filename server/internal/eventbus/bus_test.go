package eventbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/eventbus"
)

func TestPublishSubscribeAndHistory(t *testing.T) {
	bus := eventbus.New(10)
	ch := bus.Subscribe(4)
	defer bus.Unsubscribe(ch)
	ev := bus.Publish(context.Background(), eventbus.TypeJobStatus, map[string]string{"job_id": "J1"})
	select {
	case got := <-ch:
		if got.ID != ev.ID || got.Type != eventbus.TypeJobStatus {
			t.Fatalf("%+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("超时未收到")
	}
	if len(bus.Recent(1)) != 1 {
		t.Fatal("历史缺失")
	}
}
