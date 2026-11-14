package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestInfrastructureMaintenancePausesDispatchAndSeesAssignedWork(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(5)
	employees := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := employees.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1", WorkspaceID: "W1"}, "u", "")
	jobs := job.NewService(job.NewMemoryStore(), aud, bus)
	j, _, _ := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "maintenance"}, "u", "")
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	ws := workstation.NewService(ca, presence, nil)
	ws.EnsureRegistered(ctx, "WS-1", "node")
	presence.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	sched := scheduler.New(jobs, employees, ws, presence, &fakePusher{})
	sched.Maintenance = func(context.Context, string) (bool, error) { return true, nil }
	if _, err := sched.ScheduleJob(ctx, j.ID); err != scheduler.ErrNoWorkstation {
		t.Fatal(err)
	}
	lookupError := errors.New("store unavailable")
	sched.Maintenance = func(context.Context, string) (bool, error) { return false, lookupError }
	if got, err := sched.ScheduleJob(ctx, j.ID); !errors.Is(err, lookupError) || got == nil || got.Status != job.StatusFailed {
		t.Fatal("maintenance lookup errors must fail immediately", got, err)
	}
	sched.Maintenance = func(context.Context, string) (bool, error) { return false, nil }
	next, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "after-maintenance-error"}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sched.ScheduleJob(ctx, next.ID); err != nil {
		t.Fatal(err)
	}
	busy, err := sched.WorkstationBusy(ctx, "WS-1")
	if err != nil || !busy {
		t.Fatal(busy, err)
	}
}
