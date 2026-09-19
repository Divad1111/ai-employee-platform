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
	if _, err := svc.BindEmployee(ctx, ws.ID, e2.ID, "u", ""); err != workspace.ErrLocked {
		t.Fatalf("应独占锁定: %v", err)
	}
}

func TestWorkspaceCreateRequiresWorkstation(t *testing.T) {
	svc := workspace.NewService(workspace.NewMemoryStore(), audit.NewMemory())
	if _, err := svc.Create(context.Background(), workspace.CreateInput{Path: "/repo"}, "u", ""); err != workspace.ErrInvalidInput {
		t.Fatalf("缺少 workstation_id 应拒绝: %v", err)
	}
}
