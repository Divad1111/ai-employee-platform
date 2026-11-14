package scheduler_test

import (
	"context"
	"errors"
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/workergrpc"
)

type rejectedAssignmentStore struct {
	*job.MemoryStore
	err error
}

func (s *rejectedAssignmentStore) Save(ctx context.Context, j *job.Job) error {
	if j.Status == job.StatusAssigned && j.WorkstationID != "" {
		return s.err
	}
	return s.MemoryStore.Save(ctx, j)
}

func TestAssignedJobFailsImmediatelyWhenBindingCannotBeSaved(t *testing.T) {
	s, store, j, pusher := assignmentFixture(t)
	ctx := context.Background()
	failure := errors.New("cannot bind workstation")
	s.Jobs = job.NewService(&rejectedAssignmentStore{MemoryStore: store, err: failure}, audit.NewMemory(), nil)
	called := 0
	s.OnTerminal = func(ctx context.Context, failed *job.Job) {
		called++
		// Terminal callbacks run after releasing the dispatch mutex.
		if busy, err := s.WorkstationBusy(ctx, "WS"); err != nil || busy || failed.Status != job.StatusFailed {
			t.Fatalf("failed assignment retained employee: busy=%v error=%v job=%+v", busy, err, failed)
		}
	}
	got, err := s.ScheduleJob(ctx, j.ID)
	if !errors.Is(err, failure) || got == nil || got.Status != job.StatusFailed || called != 1 || len(pusher.cmds) != 0 {
		t.Fatalf("assignment failure was not terminal: job=%+v error=%v callback=%d", got, err, called)
	}
}

type queuedDispatchFailure struct {
	*workergrpc.Server
	err    error
	cancel context.CancelFunc
}

func (p *queuedDispatchFailure) PushCommand(ws string, typ aiev1.CommandType, employee, id, payload string) (*aiev1.Command, error) {
	cmd, err := p.Server.PushCommand(ws, typ, employee, id, payload)
	if err == nil && typ == aiev1.CommandType_COMMAND_TYPE_START_JOB && p.err != nil {
		if p.cancel != nil {
			p.cancel()
		}
		return cmd, p.err
	}
	return cmd, err
}

func TestDispatchFailureStopsQueuedStartAndReleasesCapacity(t *testing.T) {
	s, _, j, _ := assignmentFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	failure := errors.New("start send failed")
	pusher := &queuedDispatchFailure{Server: workergrpc.NewServer(workergrpc.Options{}), err: failure, cancel: cancel}
	s.Pusher = pusher
	s.MaxConcurrentPerWS = 1
	called := 0
	s.OnTerminal = func(ctx context.Context, failed *job.Job) {
		called++
		if ctx.Err() != nil || failed.Status != job.StatusFailed {
			t.Fatalf("failure cleanup inherited request cancellation: %v", ctx.Err())
		}
	}
	got, err := s.ScheduleJob(ctx, j.ID)
	if !errors.Is(err, failure) || got == nil || got.Status != job.StatusFailed || called != 1 {
		t.Fatalf("dispatch failure was not terminal: job=%+v error=%v callback=%d", got, err, called)
	}
	if pusher.StartCommandUnacked("WS", j.ID) {
		t.Fatal("failed task can still restart on reconnect")
	}
	pending := pusher.Commands.UnackedAfter("WS", 0)
	if len(pending) != 2 {
		t.Fatalf("missing stop commands: %+v", pending)
	}
	for _, command := range pending {
		if command.GetType() != aiev1.CommandType_COMMAND_TYPE_STOP_JOB {
			t.Fatalf("failed task retained executable start: %+v", command)
		}
	}
	pusher.err = nil
	next, _, err := s.Jobs.Create(context.Background(), job.CreateInput{EmployeeID: j.EmployeeID, WorkspaceID: "W", IdempotencyKey: "after-failure"}, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ScheduleJob(context.Background(), next.ID); err != nil || got.Status != job.StatusStarting {
		t.Fatalf("dispatch failure leaked employee or workstation capacity: job=%+v error=%v", got, err)
	}
}

func TestQueuedPreflightErrorIsTerminalAndNeverRetriedByTick(t *testing.T) {
	s, _, j, pusher := assignmentFixture(t)
	ctx := context.Background()
	if _, err := s.Jobs.Transition(ctx, j.ID, job.StatusQueued, "scheduler", "", nil); err != nil {
		t.Fatal(err)
	}
	s.SetTaskQueue(&failingContext{err: errors.New("context lookup failed")})
	if _, err := s.ScheduleJob(ctx, j.ID); err == nil {
		t.Fatal("missing dispatch error")
	}
	current, _ := s.Jobs.Get(ctx, j.ID)
	if current.Status != job.StatusFailed {
		t.Fatalf("queued task did not fail: %s", current.Status)
	}
	s.SetTaskQueue(&failingContext{})
	if scheduled := s.Tick(ctx); scheduled != 0 || len(pusher.cmds) != 0 {
		t.Fatal("failed task was automatically retried")
	}
}
