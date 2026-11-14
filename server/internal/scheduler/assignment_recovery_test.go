package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/taskconversation"
	"github.com/ai-employee-platform/server/internal/workstation"
)

type failingContext struct{ err error }

func (f *failingContext) CanScheduleJob(context.Context, string) (bool, error) { return true, nil }
func (f *failingContext) ConversationContext(context.Context, string) (string, error) {
	return "", f.err
}

func assignmentFixture(t *testing.T) (*scheduler.Service, *job.MemoryStore, *job.Job, *fakePusher) {
	t.Helper()
	ctx := context.Background()
	aud := audit.NewMemory()
	emps := employee.NewService(employee.NewMemoryStore(), aud, nil)
	e, err := emps.Create(ctx, employee.CreateInput{Name: "test", WorkstationID: "WS", WorkspaceID: "W"}, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	store := job.NewMemoryStore()
	jobs := job.NewService(store, aud, nil)
	j, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkspaceID: "W", IdempotencyKey: "first"}, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := certca.NewDevAuthority()
	if err != nil {
		t.Fatal(err)
	}
	presence := reliability.NewPresence(5, 15)
	nodes := workstation.NewService(ca, presence, nil)
	nodes.EnsureRegistered(ctx, "WS", "node")
	presence.Touch("WS", "v", 0, 0, 0, 0, 0)
	pusher := &fakePusher{}
	return scheduler.New(jobs, emps, nodes, presence, pusher), store, j, pusher
}

func TestContextFailureImmediatelyFailsJob(t *testing.T) {
	s, _, j, pusher := assignmentFixture(t)
	ctx := context.Background()
	lookupError := errors.New("database unavailable")
	s.SetTaskQueue(&failingContext{err: lookupError})
	called := 0
	s.OnTerminal = func(context.Context, *job.Job) { called++ }
	if _, err := s.ScheduleJob(ctx, j.ID); !errors.Is(err, lookupError) {
		t.Fatalf("expected lookup failure: %v", err)
	}
	current, _ := s.Jobs.Get(ctx, j.ID)
	if current.Status != job.StatusFailed || current.CompletedAt.IsZero() || len(pusher.cmds) != 0 || called != 1 {
		t.Fatalf("preflight failure was not terminal: status=%s commands=%d callback=%d", current.Status, len(pusher.cmds), called)
	}
	if _, err := s.ScheduleJob(ctx, j.ID); !errors.Is(err, job.ErrInvalidTransition) || called != 1 {
		t.Fatalf("failed job must not retry: error=%v callback=%d", err, called)
	}
	events, _ := s.Jobs.ListEvents(ctx, j.ID)
	last := events[len(events)-1]
	if last.EventType != "STATUS" || last.Payload["error"] == "" || last.Payload["phase"] != "scheduling" {
		t.Fatalf("missing scheduling failure detail: %+v", last)
	}
}

func TestWebJobWithoutFeishuConversationSchedules(t *testing.T) {
	s, _, j, pusher := assignmentFixture(t)
	s.SetTaskQueue(taskconversation.NewService(taskconversation.NewMemoryStore(), nil, s.Jobs, nil))
	got, err := s.ScheduleJob(context.Background(), j.ID)
	if err != nil || got.Status != job.StatusStarting || len(pusher.cmds) != 1 {
		t.Fatalf("unmanaged job failed: job=%v error=%v commands=%d", got, err, len(pusher.cmds))
	}
}

func TestRecoverLegacyContextFailureWithoutReclaimingLiveAssignments(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		age          time.Duration
		wantFailed   bool
	}{
		{"stale context failure", "读取员工会话上下文失败: 任务会话不存在", 2 * time.Minute, true},
		{"recent assignment failure", "读取员工会话上下文失败: 任务会话不存在", time.Second, true},
		{"no recorded failure", "", 2 * time.Minute, false},
		{"ambiguous queued send failure", "下发失败（已入队）: disconnected", 2 * time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, store, j, pusher := assignmentFixture(t)
			ctx := context.Background()
			j.Status, j.WorkstationID, j.CreatedAt = job.StatusAssigned, "WS", time.Now().Add(-time.Hour)
			if err := store.Save(ctx, j); err != nil {
				t.Fatal(err)
			}
			if err := store.AppendEvent(ctx, &job.Event{JobID: j.ID, EventType: "STATUS", Payload: map[string]string{"to": job.StatusAssigned}, CreatedAt: time.Now().Add(-tc.age)}); err != nil {
				t.Fatal(err)
			}
			if tc.reason != "" {
				if err := s.Jobs.ReportSchedulingBlock(ctx, j.ID, tc.reason); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			s.OnTerminal = func(context.Context, *job.Job) { called = true }
			s.Tick(ctx)
			current, _ := s.Jobs.Get(ctx, j.ID)
			if (current.Status == job.StatusFailed) != tc.wantFailed || called != tc.wantFailed {
				t.Fatalf("unexpected recovery: status=%s callback=%v", current.Status, called)
			}
			if tc.wantFailed {
				next, _, err := s.Jobs.Create(ctx, job.CreateInput{EmployeeID: j.EmployeeID, WorkspaceID: "W", IdempotencyKey: "next"}, "owner", "")
				if err != nil {
					t.Fatal(err)
				}
				if got, err := s.ScheduleJob(ctx, next.ID); err != nil || got.Status != job.StatusStarting || len(pusher.cmds) != 1 {
					t.Fatalf("employee still blocked: job=%v error=%v", got, err)
				}
			}
		})
	}
}
