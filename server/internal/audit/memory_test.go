package audit_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
)

func TestAuditQueryFilter(t *testing.T) {
	m := audit.NewMemory()
	m.Log(context.Background(), "USER", "u1", "employee.create", "success", "1.1.1.1", nil)
	m.Log(context.Background(), "USER", "u2", "job.create", "success", "1.1.1.1", nil)
	m.Log(context.Background(), "USER", "u1", "job.create", "success", "1.1.1.1", nil)
	got := m.Query(audit.Filter{Actor: "u1", Action: "job.create", Limit: 10})
	if len(got) != 1 {
		t.Fatalf("len=%d", len(got))
	}
	if len(m.List(2)) != 2 {
		t.Fatal("List")
	}
}
