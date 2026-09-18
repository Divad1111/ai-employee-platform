package heartbeat_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/controlplane/heartbeat"
)

func TestHeartbeatLoopSends(t *testing.T) {
	var n atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loop := &heartbeat.Loop{
		WorkstationID: "WS-1",
		Version:       "t",
		Interval:      50 * time.Millisecond,
		Send: func(ctx context.Context, hb *aiev1.Heartbeat) error {
			n.Add(1)
			if n.Load() >= 2 {
				cancel()
			}
			return nil
		},
	}
	_ = loop.Run(ctx)
	if n.Load() < 2 {
		t.Fatalf("心跳次数=%d", n.Load())
	}
}
