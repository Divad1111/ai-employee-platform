package workstation_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestMemorySharePublicRolesAndList(t *testing.T) {
	ctx := context.Background()
	store := workstation.NewMemoryShare()
	if err := store.Save(ctx, workstation.Share{
		WorkstationID: "WS-PUB",
		IsPublic:      true,
		CreatedBy:     "u-owner",
		Roles:         []string{" operator ", "OPERATOR", "", "USER"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, "WS-PUB")
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsPublic || got.CreatedBy != "u-owner" || len(got.Roles) != 2 {
		t.Fatalf("公用授权应去重空白角色: %+v", got)
	}
	got.Roles[0] = "TAMPER"
	again, _ := store.Get(ctx, "WS-PUB")
	if again.Roles[0] == "TAMPER" {
		t.Fatal("读取结果不应回写存储")
	}

	if err := store.Save(ctx, workstation.Share{
		WorkstationID: "WS-PRIV",
		IsPublic:      false,
		CreatedBy:     "u-owner",
		Roles:         []string{"ADMIN"},
	}); err != nil {
		t.Fatal(err)
	}
	priv, _ := store.Get(ctx, "WS-PRIV")
	if priv.IsPublic || len(priv.Roles) != 0 {
		t.Fatalf("非公用应清掉角色授权: %+v", priv)
	}

	pubs, err := store.ListPublic(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pubs) != 1 || pubs[0].WorkstationID != "WS-PUB" {
		t.Fatalf("只应列出公用工作站: %+v", pubs)
	}
}

func TestRoleGrantedCaseInsensitive(t *testing.T) {
	if !workstation.RoleGranted([]string{"Operator"}, []string{" operator "}) {
		t.Fatal("角色比较应忽略大小写和空白")
	}
	if workstation.RoleGranted([]string{"VIEWER"}, []string{"OPERATOR"}) {
		t.Fatal("未授权角色不应命中")
	}
	if workstation.RoleGranted(nil, []string{"USER"}) || workstation.RoleGranted([]string{"USER"}, []string{" ", ""}) {
		t.Fatal("空角色不应命中")
	}
}
