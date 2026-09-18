package employee_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
)

func TestEmployeeCRUDAndAudit(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(50)
	svc := employee.NewService(employee.NewMemoryStore(), aud, bus)

	e, err := svc.Create(ctx, employee.CreateInput{Name: "Alice", DefaultProvider: "cursor"}, "u1", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != employee.StatusStopped {
		t.Fatal(e.Status)
	}
	name := "Alice-2"
	e2, err := svc.Update(ctx, e.ID, employee.UpdateInput{Name: &name}, "u1", "1.1.1.1")
	if err != nil || e2.Name != "Alice-2" {
		t.Fatalf("%v %v", e2, err)
	}
	if _, err := svc.Disable(ctx, e.ID, "u1", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := svc.Get(ctx, e.ID)
	if got.Status != employee.StatusDisabled {
		t.Fatal(got.Status)
	}
	if err := svc.Delete(ctx, e.ID, "u1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, e.ID); err != employee.ErrNotFound {
		t.Fatal(err)
	}
	logs := aud.Query(audit.Filter{Action: "employee.create", Limit: 10})
	if len(logs) == 0 {
		t.Fatal("缺少审计")
	}
	if len(bus.Recent(10)) == 0 {
		t.Fatal("应发布事件")
	}
}
