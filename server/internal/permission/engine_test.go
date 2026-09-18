package permission_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/permission"
)

func TestDefaultDecide(t *testing.T) {
	store := permission.NewMemoryStore()
	if err := permission.EnsureDefault(store); err != nil {
		t.Fatal(err)
	}
	aud := audit.NewMemory()
	eng := permission.NewEngine(store, aud)
	ctx := context.Background()

	cases := []struct {
		action, want string
		critical     bool
	}{
		{permission.ActionWorkspaceRead, permission.EffectAllow, false},
		{permission.ActionGitPush, permission.EffectAsk, true},
		{permission.ActionSystemShutdown, permission.EffectDeny, true},
		{"unknown.action", permission.EffectDeny, false},
	}
	for _, c := range cases {
		d := eng.Decide(ctx, permission.Request{
			ActorType: "EMPLOYEE", ActorID: "EMP-1", Action: c.action,
		})
		if d.Effect != c.want {
			t.Fatalf("%s: got %s want %s", c.action, d.Effect, c.want)
		}
		if d.Critical != c.critical {
			t.Fatalf("%s critical=%v", c.action, d.Critical)
		}
	}
	// ASK/DENY 必须有审计；ALLOW 默认可不记
	q := aud.Query(audit.Filter{Action: "permission.decide", Limit: 50})
	if len(q) < 2 {
		t.Fatalf("期望至少 ASK/DENY 审计, got %d", len(q))
	}
}

func TestExportRules(t *testing.T) {
	store := permission.NewMemoryStore()
	_ = permission.EnsureDefault(store)
	eng := permission.NewEngine(store, nil)
	rules, err := eng.ExportRules(context.Background(), "")
	if err != nil || len(rules) < 9 {
		t.Fatal(err, len(rules))
	}
}
