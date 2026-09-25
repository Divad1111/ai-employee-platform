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
	m.Log(context.Background(), "USER", "uuid-1", "role.update", "success", "10.0.0.8", map[string]string{
		"summary":  "角色 ADMIN 的权限 job.write：范围 仅本人 → 全部资源",
		"username": "ada",
	})
	if got := m.Query(audit.Filter{Keyword: "10.0.0.8", Limit: 10}); len(got) != 1 {
		t.Fatalf("ip search %d", len(got))
	}
	if got := m.Query(audit.Filter{Keyword: "ada", Limit: 10}); len(got) != 1 || got[0].Action != "role.update" {
		t.Fatalf("actor/meta search %+v", got)
	}
	if got := m.Query(audit.Filter{Keyword: "job.write", Limit: 10}); len(got) != 1 {
		t.Fatalf("summary search %d", len(got))
	}
	if !audit.EntryMatches(m.List(1)[0], "ADMIN") {
		t.Fatal("EntryMatches")
	}
}
