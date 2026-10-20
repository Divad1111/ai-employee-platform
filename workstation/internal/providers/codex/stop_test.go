package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type stopRPCWriter struct {
	s       *appServerSession
	ack     bool
	written chan struct{}
}

func (p *stopRPCWriter) Close() error { return nil }
func (p *stopRPCWriter) Write(data []byte) (int, error) {
	var req struct{ ID int }
	if err := json.Unmarshal(data, &req); err != nil {
		return 0, err
	}
	if p.ack {
		p.s.mu.Lock()
		ch := p.s.pending[req.ID]
		delete(p.s.pending, req.ID)
		p.s.mu.Unlock()
		ch <- rpcMsg{Result: json.RawMessage(`{"turn":{"id":"turn-1"}}`)}
	}
	close(p.written)
	return len(data), nil
}

func TestStopAndDisconnectReleasePendingCalls(t *testing.T) {
	for _, phase := range []string{"rpc", "turn"} {
		for _, action := range []string{"stop", "disconnect"} {
			t.Run(phase+"/"+action, func(t *testing.T) {
				s := newAppServerSession("stop-test", "unused", t.TempDir(), "")
				s.ready = true
				s.threadID = "thread-1"
				peer := &stopRPCWriter{s: s, ack: phase == "turn", written: make(chan struct{})}
				s.stdin = peer
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := s.Send(ctx, []byte("read file")); done <- err }()
				select {
				case <-peer.written:
				case <-time.After(time.Second):
					t.Fatal("request was not sent")
				}
				if action == "stop" {
					_ = s.Stop(ctx)
					_ = s.Stop(ctx) // Repeated cleanup must be safe.
				} else {
					s.read(strings.NewReader(""))
				}
				select {
				case err := <-done:
					if err == nil || err == context.DeadlineExceeded {
						t.Fatalf("expected immediate lifecycle error, got %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("stopped call remained blocked")
				}
				if s.Ready() {
					t.Fatal("closed session still ready")
				}
				if _, err := s.request(ctx, "turn/start", nil); err == nil {
					t.Fatal("closed session accepted another request")
				}
			})
		}
	}
}
