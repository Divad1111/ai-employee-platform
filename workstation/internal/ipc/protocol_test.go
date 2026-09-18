package ipc_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/ipc"
)

func TestMemoryIPCRoundTrip(t *testing.T) {
	tr := ipc.NewMemoryTransport("mem-test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := &ipc.Server{
		Transport: tr,
		Handler: func(_ context.Context, req ipc.Request) ipc.Response {
			b, _ := json.Marshal(map[string]string{"echo": req.Method})
			return ipc.Response{OK: true, Result: b}
		},
	}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ctx) }()
	time.Sleep(20 * time.Millisecond)
	cli := &ipc.Client{Transport: tr}
	raw, err := cli.Call(context.Background(), "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	_ = json.Unmarshal(raw, &m)
	if m["echo"] != "hello" {
		t.Fatal(m)
	}
	cancel()
	select {
	case <-errCh:
	case <-time.After(2 * time.Second):
	}
}
