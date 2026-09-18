package session_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/session"
)

func TestSessionActiveLimitAndTransitions(t *testing.T) {
	ctx := context.Background()
	svc := session.NewService(session.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	s1, err := svc.Create(ctx, session.CreateInput{EmployeeID: "EMP-1", Provider: "cursor"}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(ctx, session.CreateInput{EmployeeID: "EMP-1"}, "u", ""); err != session.ErrActiveLimit {
		t.Fatalf("应限制 Active Session: %v", err)
	}
	if _, err := svc.Transition(ctx, s1.ID, session.StatusReady, "u", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, s1.ID, session.StatusBusy, "u", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, s1.ID, session.StatusStopping, "u", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, s1.ID, session.StatusStopped, "u", ""); err != nil {
		t.Fatal(err)
	}
	// 停止后再创建允许
	if _, err := svc.Create(ctx, session.CreateInput{EmployeeID: "EMP-1"}, "u", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, s1.ID, session.StatusBusy, "u", ""); err == nil {
		t.Fatal("STOPPED→BUSY 应拒绝")
	}
}
