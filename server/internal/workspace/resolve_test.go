package workspace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/workspace"
)

func TestResolveBindingPathCreatesWorkspace(t *testing.T) {
	ctx := context.Background()
	svc := workspace.NewService(workspace.NewMemoryStore(), audit.NewMemory())
	id, err := svc.ResolveBinding(ctx, `F:\repo\app`, "WSN-1", "EMP-1", "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if id == `F:\repo\app` || id == "" {
		t.Fatalf("应生成工作区 id，得到 %q", id)
	}
	ws, err := svc.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Path != `F:\repo\app` || ws.WorkstationID != "WSN-1" || ws.EmployeeID != "EMP-1" {
		t.Fatalf("工作区字段不符: %+v", ws)
	}
	again, err := svc.ResolveBinding(ctx, `f:/repo/app/`, "WSN-1", "EMP-1", "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if again != id {
		t.Fatalf("相同路径应复用，得到 %s 与 %s", again, id)
	}
	list, _ := svc.List(ctx)
	if len(list) != 1 {
		t.Fatalf("不应重复登记，数量 %d", len(list))
	}
}

func TestResolveBindingRejectsUnknownID(t *testing.T) {
	ctx := context.Background()
	svc := workspace.NewService(workspace.NewMemoryStore(), audit.NewMemory())
	_, err := svc.ResolveBinding(ctx, "not-a-workspace", "WSN-1", "EMP-1", "u", "")
	if !errors.Is(err, workspace.ErrWorkspaceMissing) {
		t.Fatalf("got %v", err)
	}
}

func TestResolveBindingPathNeedsWorkstation(t *testing.T) {
	ctx := context.Background()
	svc := workspace.NewService(workspace.NewMemoryStore(), audit.NewMemory())
	_, err := svc.ResolveBinding(ctx, `F:\repo`, "", "EMP-1", "u", "")
	if !errors.Is(err, workspace.ErrPathNeedsWorkstation) {
		t.Fatalf("got %v", err)
	}
}
