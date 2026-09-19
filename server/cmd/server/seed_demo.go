package main

import (
	"context"
	"fmt"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/knowledge"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/skill"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

// seedDemoData 开发环境预置可演示数据（设计 §9 Dashboard 不为空）。
func seedDemoData(
	empSvc *employee.Service,
	wsSvc *workspace.Service,
	wsNode *workstation.Service,
	presence *reliability.Presence,
	jobSvc *job.Service,
	feishuSvc *feishu.Service,
	skillSvc *skill.Service,
	knwSvc *knowledge.Service,
) {
	ctx := context.Background()
	wsNode.EnsureRegistered(ctx, "WS-DEMO-01", "Dev Laptop")
	wsNode.EnsureRegistered(ctx, "WS-DEMO-02", "Build Box")
	presence.Touch("WS-DEMO-01", "0.0.1-dev", 1, 1, 32, 48, 55)
	presence.Touch("WS-DEMO-02", "0.0.1-dev", 0, 0, 71, 82, 40)

	w1, err := wsSvc.Create(ctx, workspace.CreateInput{
		WorkstationID: "WS-DEMO-01", Path: "F:/Projects/unity", Repository: "unity-client", Branch: "main",
	}, "system", "")
	if err != nil {
		fmt.Printf("seed workspace: %v\n", err)
		return
	}
	emp, err := empSvc.Create(ctx, employee.CreateInput{
		Name: "Unity Developer", RoleSummary: "客户端开发", DefaultProvider: "cursor",
		WorkstationID: "WS-DEMO-01", WorkspaceID: w1.ID, PermissionProfile: "default",
	}, "system", "")
	if err != nil {
		fmt.Printf("seed employee: %v\n", err)
		return
	}
	emp2, _ := empSvc.Create(ctx, employee.CreateInput{
		Name: "QA Employee", RoleSummary: "质量保障", DefaultProvider: "codex",
		WorkstationID: "WS-DEMO-02", PermissionProfile: "default",
	}, "system", "")

	sk, _ := skillSvc.Create(ctx, "Unity Build", "触发本地 Unity 构建", "build")
	sk2, _ := skillSvc.Create(ctx, "Git Review", "代码审查与提交建议", "git")
	if sk != nil {
		_ = skillSvc.Bind(ctx, emp.ID, sk.ID)
	}
	if sk2 != nil {
		_ = skillSvc.Bind(ctx, emp.ID, sk2.ID)
	}

	kn, _ := knwSvc.Create(ctx, "项目约定", "分支与提交流程", "main 保护；feature/* 经 PR", []string{"git", "流程"})
	if kn != nil {
		_ = knwSvc.Bind(ctx, emp.ID, kn.ID)
	}

	feishuSvc.SetConfig(feishu.Config{AppID: "cli_demo", Enabled: true, VerificationToken: "demo-token"})
	feishuSvc.UpsertBinding(feishu.Binding{
		EmployeeID: emp.ID, FeishuAlias: "unity", FeishuOpenID: "ou_demo", ChatID: "oc_demo",
	})
	if emp2 != nil {
		feishuSvc.UpsertBinding(feishu.Binding{EmployeeID: emp2.ID, FeishuAlias: "qa"})
	}

	j1, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: emp.ID, WorkspaceID: w1.ID, Prompt: "修复登录闪退", IdempotencyKey: "demo-job-1",
	}, "system", "")
	if err == nil && j1 != nil {
		_, _ = jobSvc.Transition(ctx, j1.ID, job.StatusQueued, "system", "", nil)
		_, _ = jobSvc.Transition(ctx, j1.ID, job.StatusAssigned, "system", "", nil)
		_, _ = jobSvc.Transition(ctx, j1.ID, job.StatusStarting, "system", "", nil)
		_, _ = jobSvc.Transition(ctx, j1.ID, job.StatusRunning, "system", "", nil)
	}
	_, _, _ = jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: emp.ID, WorkspaceID: w1.ID, Prompt: "补充单元测试", IdempotencyKey: "demo-job-2",
	}, "system", "")
	fmt.Println("已注入开发演示数据（Employee / Workstation / Job / Skill / Feishu）")
}
