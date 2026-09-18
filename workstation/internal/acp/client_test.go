package acp_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/acp"
)

func TestFakeACPHandshakeAndSend(t *testing.T) {
	c := &acp.FakeClient{}
	s := c.Open("s1")
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.Ready() {
		t.Fatal("应 READY")
	}
	if err := s.Send(context.Background(), []byte("hi")); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-s.Events():
		if ev.Type == "" {
			t.Fatal(ev)
		}
	default:
		// 可能已被 Start 的 ready 占满，再读一次
		<-s.Events()
	}
	_ = s.Stop(context.Background())
	if s.Ready() {
		t.Fatal("停止后不应 Ready")
	}
}

func TestHandshakeFailure(t *testing.T) {
	c := &acp.FakeClient{FailHandshake: true}
	s := c.Open("s2")
	if err := s.Start(context.Background()); err != acp.ErrHandshake {
		t.Fatal(err)
	}
}
