package workspace_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/workspace"
)

func TestWorkspaceExclusiveBind(t *testing.T) {
	ctx := context.Background()
	empStore := employee.NewMemoryStore()
	empSvc := employee.NewService(empStore, audit.NewMemory(), eventbus.New(5))
	e1, _ := empSvc.Create(ctx, employee.CreateInput{Name: "A"}, "u", "")
	e2, _ := empSvc.Create(ctx, employee.CreateInput{Name: "B"}, "u", "")

	svc := workspace.NewService(workspace.NewMemoryStore(), audit.NewMemory())
	svc.SetBinder(workspace.EmployeeBridge{
		GetByWorkspace: func(ctx context.Context, wsID string) (string, error) {
			e, err := empStore.FindByWorkspace(ctx, wsID)
			if e == nil {
				return "", err
			}
			return e.ID, nil
		},
		SetWorkspace: func(ctx context.Context, empID, wsID string) error {
			e, _ := empStore.Get(ctx, empID)
			e.WorkspaceID = wsID
			return empStore.Save(ctx, e)
		},
	})
	ws, err := svc.Create(ctx, workspace.CreateInput{WorkstationID: "WSN-1", Path: "/repo", Repository: "r", Branch: "main"}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindEmployee(ctx, ws.ID, e1.ID, "u", ""); err != nil {
		t.Fatal(err)
	}
	rebound, err := svc.BindEmployee(ctx, ws.ID, e2.ID, "u", "")
	if err != nil {
		t.Fatalf("应允许改绑到其他员工: %v", err)
	}
	if rebound.EmployeeID != e2.ID {
		t.Fatalf("工作区应改挂到新员工: %s", rebound.EmployeeID)
	}
	if again, _ := empStore.Get(ctx, e1.ID); again.WorkspaceID != "" {
		t.Fatalf("原员工应被解除: %s", again.WorkspaceID)
	}
	if again, _ := empStore.Get(ctx, e2.ID); again.WorkspaceID != ws.ID {
		t.Fatalf("新员工应挂上该工作区: %s", again.WorkspaceID)
	}
	ws2, err := svc.Create(ctx, workspace.CreateInput{WorkstationID: "WSN-1", Path: "/other"}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BindEmployee(ctx, ws2.ID, e2.ID, "u", ""); err != nil {
		t.Fatal(err)
	}
	if moved, _ := svc.Get(ctx, ws.ID); moved.EmployeeID != "" {
		t.Fatalf("员工改挂后，原工作区应空出来: %s", moved.EmployeeID)
	}
	cleared, err := svc.UnbindEmployee(ctx, ws.ID, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if cleared.EmployeeID != "" {
		t.Fatalf("解绑后工作区不应再挂员工: %s", cleared.EmployeeID)
	}
	if again, _ := empStore.Get(ctx, e1.ID); again.WorkspaceID != "" {
		t.Fatalf("解绑后员工不应再挂工作区: %s", again.WorkspaceID)
	}
	if _, err := svc.BindEmployee(ctx, ws.ID, e2.ID, "u", ""); err != nil {
		t.Fatalf("解绑后应能绑定其他员工: %v", err)
	}
}

func TestWorkspaceCreateRequiresWorkstation(t *testing.T) {
	svc := workspace.NewService(workspace.NewMemoryStore(), audit.NewMemory())
	if _, err := svc.Create(context.Background(), workspace.CreateInput{Path: "/repo"}, "u", ""); err != workspace.ErrInvalidInput {
		t.Fatalf("缺少 workstation_id 应拒绝: %v", err)
	}
}
