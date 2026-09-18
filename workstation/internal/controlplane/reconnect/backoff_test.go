package reconnect_test

import (
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/controlplane/reconnect"
)

func TestBackoffStatesAndCap(t *testing.T) {
	b := reconnect.NewBackoff(4 * time.Second)
	if b.State() != reconnect.StateDisconnected {
		t.Fatal(b.State())
	}
	d1 := b.BeginReconnect()
	if b.State() != reconnect.StateReconnecting || d1 != time.Second {
		t.Fatalf("d1=%v state=%s", d1, b.State())
	}
	d2 := b.BeginReconnect()
	if d2 != 2*time.Second {
		t.Fatalf("d2=%v", d2)
	}
	d3 := b.BeginReconnect()
	if d3 != 4*time.Second {
		t.Fatalf("d3=%v", d3)
	}
	d4 := b.BeginReconnect()
	if d4 != 4*time.Second {
		t.Fatalf("应封顶 4s, got %v", d4)
	}
	b.MarkConnected()
	if b.State() != reconnect.StateConnected {
		t.Fatal(b.State())
	}
	b.MarkDisconnected()
	if b.State() != reconnect.StateDisconnected {
		t.Fatal(b.State())
	}
	// 重连后重置
	d := b.BeginReconnect()
	if d != time.Second {
		t.Fatalf("重置后应为 1s, got %v", d)
	}
}
