package scheduler_test

import (
	"context"
	"strings"
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
	"github.com/ai-employee-platform/server/internal/workspace"
	"github.com/ai-employee-platform/server/internal/workstation"
)

type fakeFullPusher struct {
	cmds []*aiev1.Command
}

func (f *fakeFullPusher) PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error) {
	return f.PushCommandFull(wsID, typ, employeeID, jobID, payloadJSON, nil)
}

func (f *fakeFullPusher) PushCommandFull(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string, apply func(*aiev1.Command)) (*aiev1.Command, error) {
	c := &aiev1.Command{
		WorkstationId: wsID,
		Type:          typ,
		EmployeeId:    employeeID,
		JobId:         jobID,
		PayloadJson:   payloadJSON,
	}
	if apply != nil {
		apply(c)
	}
	f.cmds = append(f.cmds, c)
	return c, nil
}

func TestSyncSkillsCommandPush(t *testing.T) {
	ctx := context.Background()
	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	_, _, err := wf.UpsertSkillMD(ctx, `---
name: sk-sync
id: skill.sync
version: 1.0.0
---
instructions
`, []workflowmcp.SkillFile{
		{Path: "helper.py", Content: []byte("print('hello')")},
	}, "patch")
	if err != nil {
		t.Fatal(err)
	}

	sched := &scheduler.Service{}
	pusher := &fakeFullPusher{}
	sched.SetWorkflowMCP(wf, "http://127.0.0.1:8080/mcp")
	sched.SetFullPusher(pusher)

	// 1. 指定 skillIDs 同步
	if err := sched.SyncSkills(ctx, "WS-1", "EMP-1", []string{"skill.sync"}); err != nil {
		t.Fatalf("SyncSkills failed: %v", err)
	}
	if len(pusher.cmds) != 1 {
		t.Fatalf("expected 1 command, got %d", len(pusher.cmds))
	}
	cmd := pusher.cmds[0]
	if cmd.Type != aiev1.CommandType_COMMAND_TYPE_SYNC_SKILLS {
		t.Fatalf("expected COMMAND_TYPE_SYNC_SKILLS, got %v", cmd.Type)
	}
	syncPayload := cmd.GetSyncSkills()
	if syncPayload == nil || len(syncPayload.Packages) != 1 {
		t.Fatalf("unexpected sync payload: %+v", syncPayload)
	}
	pkg := syncPayload.Packages[0]
	if pkg.Id != "skill.sync" || pkg.CursorName != "sk-sync" || len(pkg.Files) != 1 {
		t.Fatalf("unexpected package details: %+v", pkg)
	}

	// 2. 授权工作流后空 skillIDs 自动解析 Employee 关联技能
	_, _, _ = wf.UpsertWorkflowYAML(ctx, `id: wf-sync
name: Sync WF
version: 1.0.0
skills:
  - skill.sync
steps:
  - id: s1
    description: d
`, "patch")
	_ = wf.GrantWorkflow(ctx, "EMP-AUTO", "wf-sync", "admin")

	if err := sched.SyncSkills(ctx, "WS-1", "EMP-AUTO", nil); err != nil {
		t.Fatalf("SyncSkills auto scope failed: %v", err)
	}
	if len(pusher.cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(pusher.cmds))
	}
	autoPayload := pusher.cmds[1].GetSyncSkills()
	if autoPayload == nil || len(autoPayload.Packages) != 1 || autoPayload.Packages[0].Id != "skill.sync" {
		t.Fatalf("unexpected auto resolved package: %+v", autoPayload)
	}
}

func TestScheduleJobWithWorkflowMCP(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(5)

	wsSvc := workspace.NewService(workspace.NewMemoryStore(), aud)
	ws, _ := wsSvc.Create(ctx, workspace.CreateInput{WorkstationID: "WS-1", Path: "/path/to/project"}, "u", "")

	empSvc := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := empSvc.Create(ctx, employee.CreateInput{Name: "Alice", WorkstationID: "WS-1", WorkspaceID: ws.ID}, "u", "")

	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	presence.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	wss := workstation.NewService(ca, presence, nil)
	wss.EnsureRegistered(ctx, "WS-1", "node-1")

	pusher := &fakeFullPusher{}
	jobSvc := job.NewService(job.NewMemoryStore(), aud, bus)

	sched := scheduler.New(jobSvc, empSvc, wss, presence, pusher)
	sched.Workspaces = wsSvc

	wf := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	_, _, _ = wf.UpsertSkillMD(ctx, `---
name: sk-job
id: skill.job
version: 1.0.0
---
body
`, nil, "patch")

	_, _, _ = wf.UpsertWorkflowYAML(ctx, `id: wf-job
name: Job Workflow
version: 1.0.0
skills:
  - skill.job
steps:
  - id: s1
    description: step
`, "patch")

	mcpAuthSvc := mcpauth.NewService(mcpauth.NewMemoryStore())
	sched.SetWorkflowMCP(wf, "http://127.0.0.1:8080/mcp")
	sched.SetMCPAuth(mcpAuthSvc)
	sched.SetFullPusher(pusher)

	// 1. 未授权工作流调度 -> 报错
	jUnauthorized, _, _ := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: e.ID, WorkspaceID: ws.ID, Prompt: "run unauth", IdempotencyKey: "k-unauth",
		WorkflowID: "wf-job",
	}, "u", "")

	_, err := sched.ScheduleJob(ctx, jUnauthorized.ID)
	if err == nil || !strings.Contains(err.Error(), "未授权工作流") {
		t.Fatalf("expected unauthorized workflow error, got %v", err)
	}

	// 2. 授权工作流后调度 -> 成功并注入 StartJobPayload
	_ = wf.GrantWorkflow(ctx, e.ID, "wf-job", "admin")

	jAuthorized, _, _ := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: e.ID, WorkspaceID: ws.ID, Prompt: "run auth", IdempotencyKey: "k-auth",
		WorkflowID: "wf-job",
	}, "u", "")

	gotJob, err := sched.ScheduleJob(ctx, jAuthorized.ID)
	if err != nil {
		t.Fatalf("ScheduleJob failed: %v", err)
	}
	if gotJob.WorkflowID != "wf-job" || gotJob.WorkflowSnapshot == nil {
		t.Fatalf("expected workflow snapshot: %+v", gotJob)
	}

	if len(pusher.cmds) == 0 {
		t.Fatal("expected pushed commands")
	}
	lastCmd := pusher.cmds[len(pusher.cmds)-1]
	startPayload := lastCmd.GetStartJob()
	if startPayload == nil {
		t.Fatalf("expected StartJob payload: %+v", lastCmd)
	}
	if startPayload.WorkflowId != "wf-job" || startPayload.WorkflowVersion != "1.0.0" {
		t.Fatalf("unexpected workflow id/ver: %+v", startPayload)
	}
	if len(startPayload.SkillPackages) != 1 || startPayload.SkillPackages[0].Id != "skill.job" {
		t.Fatalf("unexpected skill packages: %+v", startPayload.SkillPackages)
	}
	if len(startPayload.McpServers) != 1 {
		t.Fatalf("expected 1 MCP server, got %d", len(startPayload.McpServers))
	}
	mcpServer := startPayload.McpServers[0]
	if mcpServer.Name != "workflow-mcp" || !strings.HasPrefix(mcpServer.Headers["Authorization"], "Bearer aiemcp_") {
		t.Fatalf("unexpected MCPServerSpec: %+v", mcpServer)
	}
}
