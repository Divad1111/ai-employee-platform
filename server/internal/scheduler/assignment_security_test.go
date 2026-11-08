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
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestDeniedOrInconsistentAssignmentSendsNoCommand(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(5)
	emps := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := emps.Create(ctx, employee.CreateInput{Name: "test", WorkstationID: "node", WorkspaceID: "workspace"}, "owner", "")
	jobs := job.NewService(job.NewMemoryStore(), aud, bus)
	j, _, _ := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkspaceID: "workspace", Prompt: "test", IdempotencyKey: "probe-1"}, "owner", "")
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	nodes := workstation.NewService(ca, presence, nil)
	nodes.EnsureRegistered(ctx, "node", "node")
	presence.Touch("node", "v", 0, 0, 0, 0, 0)
	pusher := &fakePusher{}
	s := scheduler.New(jobs, emps, nodes, presence, pusher)
	denied := errors.New("revoked")
	s.AuthorizeAssignment = func(context.Context, *job.Job, *employee.Employee, string) error { return denied }
	if _, err := s.ScheduleJob(ctx, j.ID); !errors.Is(err, denied) {
		t.Fatal(err)
	}
	if len(pusher.cmds) != 0 {
		t.Fatal("unauthorized command sent")
	}
	ws := workspace.NewService(workspace.NewMemoryStore(), aud)
	w, _ := ws.Create(ctx, workspace.CreateInput{WorkstationID: "other-node", Path: "/project"}, "owner", "")
	_, _ = emps.Update(ctx, e.ID, employee.UpdateInput{WorkspaceID: &w.ID}, "owner", "")
	j2, _, _ := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkspaceID: w.ID, Prompt: "test", IdempotencyKey: "probe-2"}, "owner", "")
	s.SetWorkspaces(ws)
	s.AuthorizeAssignment = nil
	if _, err := s.ScheduleJob(ctx, j2.ID); err == nil {
		t.Fatal("cross-node workspace accepted")
	}
	if len(pusher.cmds) != 0 {
		t.Fatal("inconsistent command sent")
	}
}
