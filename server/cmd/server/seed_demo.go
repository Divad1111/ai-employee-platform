package main

import (
	"context"
	"fmt"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

// seedDemoData 开发环境预置可演示数据（含示例工作流）。
func seedDemoData(
	empSvc *employee.Service,
	wsSvc *workspace.Service,
	wsNode *workstation.Service,
	presence *reliability.Presence,
	jobSvc *job.Service,
	feishuSvc *feishu.Service,
	wfSvc *workflowmcp.Service,
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

	// 示例技能包
	skillMD := `---
name: unity-build
id: unity.build
title: Unity Build
version: 1.0.0
description: 触发本地 Unity 构建
disable-model-invocation: true
---
# Unity Build

执行本地 Unity 构建并收集日志。
`
	sk, _, err := wfSvc.UpsertSkillMD(ctx, skillMD, nil, "patch")
	if err != nil {
		fmt.Printf("seed skill: %v\n", err)
	}

	// 示例知识
	_, _, _ = wfSvc.UpsertKnowledgeMD(ctx, "git/conventions.md", `---
title: 项目约定
namespace: git
version: 1.0.0
---
# 项目约定

main 保护；feature/* 经 PR。
`, "patch")

	// 示例工作流
	wfYAML := `id: unity-dev
name: Unity 开发流程
version: 1.0.0
description: 客户端日常开发工作流
when_to_use:
  - Unity 开发
  - 客户端修复
skills:
  - unity.build
knowledge:
  - git
steps:
  - id: analyze
    description: 分析问题
  - id: implement
    description: 实现修复
  - id: verify
    description: 验证
approval:
  code_modify: true
`
	wf, _, err := wfSvc.UpsertWorkflowYAML(ctx, wfYAML, "patch")
	if err != nil {
		fmt.Printf("seed workflow: %v\n", err)
	} else if emp != nil {
		_ = wfSvc.GrantWorkflow(ctx, emp.ID, wf.ID, "system")
	}
	_ = sk

	feishuSvc.SetConfig(feishu.Config{AppID: "cli_demo", Enabled: true, VerificationToken: "demo-token"})
	feishuSvc.UpsertBinding(feishu.Binding{
		EmployeeID: emp.ID, FeishuAlias: "unity", FeishuOpenID: "ou_demo", ChatID: "oc_demo",
	})
	if emp2 != nil {
		feishuSvc.UpsertBinding(feishu.Binding{EmployeeID: emp2.ID, FeishuAlias: "qa"})
	}

	j1, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: emp.ID, WorkspaceID: w1.ID, Prompt: "修复登录闪退",
		IdempotencyKey: "demo-job-1", WorkflowID: "unity-dev", Source: job.SourceSystem,
	}, "system", "")
	if err == nil && j1 != nil {
		_, _ = jobSvc.Transition(ctx, j1.ID, job.StatusQueued, "system", "", nil)
	}
}
