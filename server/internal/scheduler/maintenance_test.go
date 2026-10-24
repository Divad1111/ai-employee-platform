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
	sched.Maintenance = func(context.Context, string) (bool, error) { return false, errors.New("store unavailable") }
	if _, err := sched.ScheduleJob(ctx, j.ID); err != scheduler.ErrNoWorkstation {
		t.Fatal("maintenance lookup must fail closed", err)
	}
	sched.Maintenance = func(context.Context, string) (bool, error) { return false, nil }
	if _, err := sched.ScheduleJob(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	busy, err := sched.WorkstationBusy(ctx, "WS-1")
	if err != nil || !busy {
		t.Fatal(busy, err)
	}
}
