package api_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/api"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/config"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestDispatchRechecksCurrentAuthorization(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	_ = users.CreateRole(ctx, "JOB_OWNER", "", []auth.PermissionGrant{{Code: "job.write", Scope: "OWN"}})
	u := &auth.User{ID: "owner", Username: "owner", Status: auth.StatusActive, Roles: []string{"JOB_OWNER"}}
	if err := users.Create(ctx, u); err != nil {
		t.Fatal(err)
	}
	a := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	shares := workstation.NewMemoryShare()
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "node", CreatedBy: "workstation-owner", IsPublic: true, Roles: []string{"JOB_OWNER"}})
	d := api.Deps{Config: &config.Config{Env: "production"}, Auth: a, WSShare: shares}
	e := &employee.Employee{ID: "emp", OwnerUserID: u.ID, WorkstationID: "node"}
	j := &job.Job{EmployeeID: e.ID, CreatedBy: u.ID}
	if err := d.AuthorizeAssignment(ctx, j, e, "node"); err != nil {
		t.Fatal(err)
	}
	for _, creator := range []string{"feishu:sender", "automation:scheduled"} {
		j.CreatedBy = creator
		if err := d.AuthorizeAssignment(ctx, j, e, "node"); err != nil {
			t.Fatal("公用生产工作站应允许授权用户的委托任务", creator, err)
		}
	}
	j.CreatedBy = u.ID
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "node", CreatedBy: "workstation-owner", IsPublic: true, Roles: []string{"OTHER_ROLE"}})
	if d.AuthorizeAssignment(ctx, j, e, "node") == nil {
		t.Fatal("public node without a matching role accepted")
	}
	// 工作站授权撤销，已经创建的任务不能继续派发。
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "node", IsPublic: false})
	if d.AuthorizeAssignment(ctx, j, e, "node") == nil {
		t.Fatal("revoked node access accepted")
	}
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "node", CreatedBy: u.ID})
	_ = users.SetRolePermissionScope(ctx, "JOB_OWNER", "job.write", "NONE")
	if d.AuthorizeAssignment(ctx, j, e, "node") == nil {
		t.Fatal("revoked job permission accepted")
	}
	_ = users.SetRolePermissionScope(ctx, "JOB_OWNER", "job.write", "OWN")
	j.CreatedBy = "automation:scheduled"
	if d.AuthorizeAssignment(ctx, j, e, "node") != nil {
		t.Fatal("valid delegated job denied")
	}
	saved, _ := users.FindByID(ctx, u.ID)
	saved.Status = auth.StatusDisabled
	_ = users.Update(ctx, saved)
	if d.AuthorizeAssignment(ctx, j, e, "node") == nil {
		t.Fatal("disabled automation owner accepted")
	}
}

func TestProductionPrivateWorkstationRemainsDedicated(t *testing.T) {
	ctx := context.Background()
	users := auth.NewMemoryUserStore()
	_ = users.SeedAdmin("admin", "secret", "Admin")
	a := auth.NewService(users, auth.NewMemorySessionStore(), audit.NewMemory())
	u, _ := users.FindByUsername(ctx, "admin")
	shares := workstation.NewMemoryShare()
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "node", CreatedBy: u.ID})
	d := api.Deps{Config: &config.Config{Env: "production"}, Auth: a, WSShare: shares}
	j := &job.Job{CreatedBy: u.ID}
	e := &employee.Employee{OwnerUserID: "other-user"}
	if d.AuthorizeAssignment(ctx, j, e, "node") == nil {
		t.Fatal("private production node accepted another owner's employee even with global job permission")
	}
	_ = shares.Save(ctx, workstation.Share{WorkstationID: "node", CreatedBy: u.ID, IsPublic: true, Roles: []string{"ADMIN"}})
	if err := d.AuthorizeAssignment(ctx, j, e, "node"); err != nil {
		t.Fatal("explicit public workstation denied", err)
	}
	e.OwnerUserID = u.ID
	if err := d.AuthorizeAssignment(ctx, j, e, "node"); err != nil {
		t.Fatal("valid dedicated workstation denied", err)
	}
	if d.AuthorizeAssignment(ctx, j, e, "unowned") == nil {
		t.Fatal("unowned workstation accepted")
	}
}
