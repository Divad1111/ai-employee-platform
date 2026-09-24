package quota

import (
	"context"
	"testing"
)

func TestCheckUser_RolePresetThenUserOverride(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := NewService(store)

	userID := "u-1"
	// 角色预设 VIEWER=1M，用量按人累计
	if err := svc.CheckUser(ctx, userID, []string{"VIEWER"}, 100, 1); err != nil {
		t.Fatalf("within role preset: %v", err)
	}
	_, _ = store.AddUsage(ctx, TypeUser, userID, PeriodMonthly, 999_950, 0)
	if err := svc.CheckUser(ctx, userID, []string{"VIEWER"}, 100, 1); err != ErrExceeded {
		t.Fatalf("want exceeded against VIEWER preset, got %v", err)
	}

	// 个人策略覆盖角色预设
	_ = store.UpsertPolicy(ctx, &Policy{
		ResourceType: TypeUser, ResourceID: userID, PeriodType: PeriodMonthly,
		TokenLimit: 2_000_000, Enabled: true,
	})
	if err := svc.CheckUser(ctx, userID, []string{"VIEWER"}, 100, 1); err != nil {
		t.Fatalf("user override should allow: %v", err)
	}
}

func TestResolveUserPolicy_MultiRoleTakesMax(t *testing.T) {
	ctx := context.Background()
	svc := NewService(NewMemoryStore())
	p, err := svc.ResolveUserPolicy(ctx, "u-2", []string{"VIEWER", "OPERATOR"})
	if err != nil {
		t.Fatal(err)
	}
	if p == nil || p.TokenLimit != 5_000_000 {
		t.Fatalf("want OPERATOR 5M limit, got %+v", p)
	}
}
