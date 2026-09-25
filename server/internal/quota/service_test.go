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

func TestEffective_ExceptionReplacesRoleAndBonusAdds(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	svc := NewService(store)
	userID := "u-bonus"

	eff, err := svc.ResolveEffective(ctx, userID, []string{"VIEWER"})
	if err != nil {
		t.Fatal(err)
	}
	if eff.TokenLimit != 1_000_000 || eff.Source != TypeRole {
		t.Fatalf("role only: %+v", eff)
	}

	_ = store.UpsertPolicy(ctx, &Policy{
		ResourceType: TypeUserBonus, ResourceID: userID, PeriodType: PeriodMonthly,
		TokenLimit: 250_000, Enabled: true,
	})
	eff, err = svc.ResolveEffective(ctx, userID, []string{"VIEWER"})
	if err != nil {
		t.Fatal(err)
	}
	if eff.TokenLimit != 1_250_000 || eff.ExtraTokenLimit != 250_000 || eff.RoleTokenLimit != 1_000_000 {
		t.Fatalf("role+extra: %+v", eff)
	}

	_ = store.UpsertPolicy(ctx, &Policy{
		ResourceType: TypeUser, ResourceID: userID, PeriodType: PeriodMonthly,
		TokenLimit: 2_000_000, Enabled: true,
	})
	eff, err = svc.ResolveEffective(ctx, userID, []string{"VIEWER"})
	if err != nil {
		t.Fatal(err)
	}
	if eff.Source != TypeUser || eff.TokenLimit != 2_250_000 || eff.ExceptionTokenLimit != 2_000_000 {
		t.Fatalf("exception replaces role then extra adds: %+v", eff)
	}
	_, _ = store.AddUsage(ctx, TypeUser, userID, PeriodMonthly, 2_200_000, 0)
	if err := svc.CheckUser(ctx, userID, []string{"VIEWER"}, 100_000, 0); err != ErrExceeded {
		t.Fatalf("want exceeded at exception+extra, got %v", err)
	}
}

func TestDeletePolicy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_ = store.UpsertPolicy(ctx, &Policy{
		ResourceType: TypeUserBonus, ResourceID: "u-del", PeriodType: PeriodMonthly,
		TokenLimit: 100, Enabled: true,
	})
	if err := store.DeletePolicy(ctx, TypeUserBonus, "u-del", PeriodMonthly); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetPolicy(ctx, TypeUserBonus, "u-del", PeriodMonthly); err != ErrNotFound {
		t.Fatalf("want not found, got %v", err)
	}
	if err := store.DeletePolicy(ctx, TypeUserBonus, "u-del", ""); err != ErrNotFound {
		t.Fatalf("repeat delete: %v", err)
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
