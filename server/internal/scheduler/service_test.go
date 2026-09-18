package scheduler_test

import (
	"context"
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/workstation"
)

type fakePusher struct {
	cmds []*aiev1.Command
}

func (f *fakePusher) PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error) {
	c := &aiev1.Command{WorkstationId: wsID, Type: typ, EmployeeId: employeeID, JobId: jobID, PayloadJson: payloadJSON}
	f.cmds = append(f.cmds, c)
	return c, nil
}

func TestScheduleRequiresOnlineWS(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(5)
	empSvc := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := empSvc.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1", WorkspaceID: "W1"}, "u", "")
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)
	j, _, _ := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: e.ID, WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "k1",
	}, "u", "")

	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	wss := workstation.NewService(ca, presence, nil)
	wss.EnsureRegistered(ctx, "WS-1", "n1")
	pusher := &fakePusher{}
	sched := scheduler.New(jobSvc, empSvc, wss, presence, pusher)

	if _, err := sched.ScheduleJob(ctx, j.ID); err != scheduler.ErrNoWorkstation {
		t.Fatalf("offline 应拒绝: %v", err)
	}
	presence.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	got, err := sched.ScheduleJob(ctx, j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != job.StatusStarting {
		t.Fatal(got.Status)
	}
	if len(pusher.cmds) != 1 || pusher.cmds[0].Type != aiev1.CommandType_COMMAND_TYPE_START_JOB {
		t.Fatal(pusher.cmds)
	}
}

func TestResourceLimitKeepsQueued(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(5)
	empSvc := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := empSvc.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1", WorkspaceID: "W1"}, "u", "")
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)
	j, _, _ := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: e.ID, WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "k-res",
	}, "u", "")
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	wss := workstation.NewService(ca, presence, nil)
	wss.EnsureRegistered(ctx, "WS-1", "n1")
	presence.Touch("WS-1", "v", 0, 0, 95, 10, 0) // CPU 95%
	sched := scheduler.New(jobSvc, empSvc, wss, presence, &fakePusher{})
	sched.MaxCPUPercent = 90
	if _, err := sched.ScheduleJob(ctx, j.ID); err == nil {
		t.Fatal("超限应拒绝")
	}
	if sched.RejectReasons()["WS-1"] == "" {
		t.Fatal("应有拒绝原因")
	}
	jj, _ := jobSvc.Get(ctx, j.ID)
	if jj.Status != job.StatusCreated {
		t.Fatal(jj.Status)
	}
}

func TestWorkspaceMissingRejects(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(5)
	empSvc := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := empSvc.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1"}, "u", "")
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)
	j, _, _ := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: e.ID, Prompt: "p", IdempotencyKey: "k-ws-miss",
	}, "u", "")
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	wss := workstation.NewService(ca, presence, nil)
	wss.EnsureRegistered(ctx, "WS-1", "n1")
	presence.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	sched := scheduler.New(jobSvc, empSvc, wss, presence, &fakePusher{})
	_, err := sched.ScheduleJob(ctx, j.ID)
	if err == nil || err.Error() != "Workspace Missing" {
		t.Fatalf("应 Workspace Missing: %v", err)
	}
}
