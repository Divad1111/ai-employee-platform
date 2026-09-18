package job_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
)

func TestJobIdempotencyAndStateMachine(t *testing.T) {
	ctx := context.Background()
	svc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(20))

	j1, dup, err := svc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-1", Prompt: "fix", IdempotencyKey: "k1", TimeoutSec: 600,
	}, "u", "")
	if err != nil || dup {
		t.Fatalf("%v dup=%v", err, dup)
	}
	j2, dup2, err := svc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-1", Prompt: "fix", IdempotencyKey: "k1",
	}, "u", "")
	if err != nil || !dup2 || j2.ID != j1.ID {
		t.Fatalf("幂等失败: %v dup=%v", err, dup2)
	}
	_, _, err = svc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-2", Prompt: "other", IdempotencyKey: "k1",
	}, "u", "")
	if err != job.ErrIdempotencyConflict {
		t.Fatalf("应冲突: %v", err)
	}

	path := []string{job.StatusQueued, job.StatusAssigned, job.StatusStarting, job.StatusRunning}
	cur := j1
	for _, st := range path {
		var e error
		cur, e = svc.Transition(ctx, cur.ID, st, "u", "", nil)
		if e != nil {
			t.Fatalf("%s: %v", st, e)
		}
	}
	// WAITING_APPROVAL 出边
	cur, err = svc.Transition(ctx, cur.ID, job.StatusWaitingApproval, "u", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cur, err = svc.Transition(ctx, cur.ID, job.StatusRunning, "u", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cur, err = svc.Transition(ctx, cur.ID, job.StatusUnknown, "u", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	cur, err = svc.Transition(ctx, cur.ID, job.StatusFailed, "u", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Transition(ctx, cur.ID, job.StatusRunning, "u", "", nil); err == nil {
		t.Fatal("终态不可再转")
	}
	tl, err := svc.Timeline(ctx, cur.ID)
	if err != nil || len(tl) < 2 {
		t.Fatalf("timeline=%d err=%v", len(tl), err)
	}
}

func TestJobHappyPathAndCancel(t *testing.T) {
	ctx := context.Background()
	svc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	j, _, err := svc.Create(ctx, job.CreateInput{EmployeeID: "E", IdempotencyKey: "c1", Prompt: "p"}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cancel(ctx, j.ID, "u", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, j.ID)
	if got.Status != job.StatusCancelled {
		t.Fatal(got.Status)
	}
}

func TestIllegalTransition(t *testing.T) {
	ctx := context.Background()
	svc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(5))
	j, _, _ := svc.Create(ctx, job.CreateInput{EmployeeID: "E", IdempotencyKey: "x", Prompt: "p"}, "u", "")
	if _, err := svc.Transition(ctx, j.ID, job.StatusRunning, "u", "", nil); err == nil {
		t.Fatal("CREATED→RUNNING 应拒绝")
	}
}

func TestJobTimeoutFromRunning(t *testing.T) {
	ctx := context.Background()
	svc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	j, _, err := svc.Create(ctx, job.CreateInput{
		EmployeeID: "E", IdempotencyKey: "to1", Prompt: "p", TimeoutSec: 30,
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if j.TimeoutSec != 30 {
		t.Fatal(j.TimeoutSec)
	}
	for _, st := range []string{job.StatusQueued, job.StatusAssigned, job.StatusStarting, job.StatusRunning} {
		if _, err := svc.Transition(ctx, j.ID, st, "u", "", nil); err != nil {
			t.Fatal(st, err)
		}
	}
	got, err := svc.Transition(ctx, j.ID, job.StatusTimeout, "system", "", map[string]string{"reason": "timeout_sec"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != job.StatusTimeout || got.CompletedAt.IsZero() {
		t.Fatal(got)
	}
	// 终态不可再转 RUNNING
	if _, err := svc.Transition(ctx, j.ID, job.StatusRunning, "u", "", nil); err == nil {
		t.Fatal("TIMEOUT 终态不可恢复为 RUNNING")
	}
}
