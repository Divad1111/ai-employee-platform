package workflowmcp_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func TestSemverBumpAndResolve(t *testing.T) {
	if workflowmcp.CompareVersions("1.0.0", "1.0.1") >= 0 {
		t.Fatal("compare")
	}
	if workflowmcp.BumpVersion("1.2.3", "minor") != "1.3.0" {
		t.Fatal("bump minor")
	}
	v := workflowmcp.ResolveSavedVersion("1.0.0", "1.0.0", false, "patch")
	if v != "1.0.1" {
		t.Fatalf("got %s", v)
	}
	v = workflowmcp.ResolveSavedVersion("1.0.0", "2.0.0", false, "patch")
	if v != "2.0.0" {
		t.Fatalf("got %s", v)
	}
}

func TestTokenizerCJK(t *testing.T) {
	toks := workflowmcp.Tokenize("Unity性能卡顿 FPS")
	joined := strings.Join(toks, " ")
	if !strings.Contains(joined, "unity") || !strings.Contains(joined, "性能") {
		t.Fatalf("tokens=%v", toks)
	}
	if workflowmcp.ScoreText("性能", "地图拖动性能问题") <= 0 {
		t.Fatal("score")
	}
}

func TestFrontMatterRoundTrip(t *testing.T) {
	raw := `---
name: demo-skill
id: demo.skill
title: Demo
version: 1.0.0
description: hello
disable-model-invocation: true
---
# Body

content here
`
	sp, err := workflowmcp.ParseSkillMD(raw)
	if err != nil {
		t.Fatal(err)
	}
	if sp.ID != "demo.skill" || sp.CursorName != "demo-skill" {
		t.Fatalf("%+v", sp)
	}
	md, err := workflowmcp.RenderSkillMD(sp)
	if err != nil || !strings.Contains(md, "demo.skill") {
		t.Fatalf("%v %s", err, md)
	}
}

func TestResolveScopeClosure(t *testing.T) {
	ctx := context.Background()
	svc := workflowmcp.NewService(workflowmcp.NewMemoryStore())
	_, _, err := svc.UpsertSkillMD(ctx, `---
name: sk-a
id: skill.a
version: 1.0.0
description: A
---
body
`, nil, "patch")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.UpsertKnowledgeMD(ctx, "unity/perf.md", `---
title: Perf
namespace: unity
---
# Perf
卡顿分析
`, "patch")
	if err != nil {
		t.Fatal(err)
	}
	wfYAML := `id: wf-1
name: W1
version: 1.0.0
skills:
  - skill.a
knowledge:
  - unity
  - "*"
steps:
  - id: s1
    description: step
`
	_, _, err = svc.UpsertWorkflowYAML(ctx, wfYAML, "patch")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.GrantWorkflow(ctx, "EMP-1", "wf-1", "admin"); err != nil {
		t.Fatal(err)
	}
	scope, err := svc.ResolveScope(ctx, "EMP-1")
	if err != nil {
		t.Fatal(err)
	}
	if !scope.InScopeWorkflow("wf-1") || !scope.InScopeSkill("skill.a") {
		t.Fatalf("%+v", scope)
	}
	if !scope.AllKnowledge || !scope.InScopeKnowledge("anything/goes") {
		t.Fatalf("all knowledge expected: %+v", scope)
	}
	eff, err := svc.EffectiveSkills(ctx, "EMP-1")
	if err != nil || len(eff) != 1 {
		t.Fatalf("%v %d", err, len(eff))
	}
}

func TestWorkflowCRUD(t *testing.T) {
	ctx := context.Background()
	svc := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	wfYAML := `id: wf-crud
name: CRUD Workflow
version: 1.0.0
description: A workflow for testing CRUD
when-to-use:
  - testing
  - crud
skills: []
knowledge: []
steps:
  - id: step-1
    description: first step
`
	// 1. Create
	wf, action, err := svc.UpsertWorkflowYAML(ctx, wfYAML, "patch")
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	if action != "created" || wf.ID != "wf-crud" || wf.Version != "1.0.0" {
		t.Fatalf("unexpected create result: action=%s wf=%+v", action, wf)
	}

	// 2. Get
	got, err := svc.GetWorkflow(ctx, "wf-crud")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if got.Name != "CRUD Workflow" {
		t.Fatalf("name mismatch: got %s", got.Name)
	}

	// 3. Search
	hits, err := svc.SearchWorkflows(ctx, "testing", 5)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "wf-crud" {
		t.Fatalf("unexpected search hits: %+v", hits)
	}

	// 4. Update (bump minor)
	wfUpdatedYAML := `id: wf-crud
name: CRUD Workflow V2
version: 1.0.0
description: updated description
when-to-use: []
skills: []
knowledge: []
steps:
  - id: step-1
    description: updated step
`
	wf2, action2, err := svc.UpsertWorkflowYAML(ctx, wfUpdatedYAML, "minor")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if action2 != "updated" || wf2.Version != "1.1.0" {
		t.Fatalf("expected version 1.1.0, got %s, action=%s", wf2.Version, action2)
	}

	// 5. List
	all, err := svc.ListWorkflows(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("expected 1 workflow, got %d (err: %v)", len(all), err)
	}

	// 6. Delete
	if err := svc.DeleteWorkflow(ctx, "wf-crud"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := svc.GetWorkflow(ctx, "wf-crud"); err != workflowmcp.ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestSkillCRUDAndExport(t *testing.T) {
	ctx := context.Background()
	svc := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	md := `---
name: code-review
id: skill.review
title: Code Review
version: 1.0.0
description: Perform strict code review
---
# Instructions
Review the diff carefully.
`
	files := []workflowmcp.SkillFile{
		{Path: "rules/go.md", Content: []byte("Always check errors.")},
	}

	// 1. Create
	sp, action, err := svc.UpsertSkillMD(ctx, md, files, "patch")
	if err != nil {
		t.Fatalf("upsert skill failed: %v", err)
	}
	if action != "created" || sp.ID != "skill.review" || sp.CursorName != "code-review" {
		t.Fatalf("unexpected result: action=%s, sp=%+v", action, sp)
	}

	// 2. Export
	spExport, mdExport, err := svc.ExportSkill(ctx, "skill.review")
	if err != nil {
		t.Fatalf("export failed: %v", err)
	}
	if spExport.CursorName != "code-review" || !strings.Contains(mdExport, "skill.review") {
		t.Fatalf("export content mismatch: %s", mdExport)
	}
	if len(spExport.Files) != 1 || spExport.Files[0].Path != "rules/go.md" {
		t.Fatalf("files mismatch: %+v", spExport.Files)
	}

	// 3. Search
	hits, err := svc.SearchSkills(ctx, "review", 5)
	if err != nil || len(hits) != 1 || hits[0].ID != "skill.review" {
		t.Fatalf("search skills failed: %v, hits=%+v", err, hits)
	}

	// 4. Delete
	if err := svc.DeleteSkill(ctx, "skill.review"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if _, err := svc.GetSkill(ctx, "skill.review"); err != workflowmcp.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestKnowledgeAndReindex(t *testing.T) {
	ctx := context.Background()
	svc := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	content1 := `---
title: Go Best Practices
namespace: backend
---
# Go Guide
Writing clean goroutines and error handling.
`
	content2 := `---
title: Crash Incident Postmortem
namespace: cases
---
# Case 101
OOM crash caused by memory leak in buffer pool.
`
	// 1. Upsert Knowledge
	doc1, action1, err := svc.UpsertKnowledgeMD(ctx, "backend/go.md", content1, "patch")
	if err != nil || action1 != "created" {
		t.Fatalf("upsert doc1 failed: %v action=%s", err, action1)
	}
	_, _, err = svc.UpsertKnowledgeMD(ctx, "cases/oom.md", content2, "patch")
	if err != nil {
		t.Fatalf("upsert doc2 failed: %v", err)
	}

	// 2. Get
	got, err := svc.GetKnowledge(ctx, doc1.ID)
	if err != nil || got.Title != "Go Best Practices" {
		t.Fatalf("get doc1 failed: %v, got=%+v", err, got)
	}

	// 3. Search Knowledge with namespace
	hits, err := svc.SearchKnowledge(ctx, "goroutines", "backend", 5)
	if err != nil || len(hits) == 0 {
		t.Fatalf("search backend failed: %v, hits=%+v", err, hits)
	}

	// 4. Search Cases
	casesHits, err := svc.SearchCases(ctx, "memory leak", 5)
	if err != nil || len(casesHits) == 0 {
		t.Fatalf("search cases failed: %v, hits=%+v", err, casesHits)
	}

	// 5. Reindex
	count, err := svc.ReindexKnowledge(ctx)
	if err != nil || count != 2 {
		t.Fatalf("reindex failed: count=%d, err=%v", count, err)
	}

	// 6. Delete
	if err := svc.DeleteKnowledge(ctx, doc1.ID); err != nil {
		t.Fatalf("delete knowledge failed: %v", err)
	}
	if _, err := svc.GetKnowledge(ctx, doc1.ID); err != workflowmcp.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSkillVersionCheck(t *testing.T) {
	ctx := context.Background()
	svc := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	_, _, _ = svc.UpsertSkillMD(ctx, `---
name: sk-chk
id: skill.check
version: 1.2.0
---
body
`, nil, "patch")

	// 1. CheckSkill: same version
	chk1, err := svc.CheckSkill(ctx, "skill.check", "1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if chk1["status"] != "current" {
		t.Fatalf("expected current, got %v", chk1["status"])
	}

	// 2. CheckSkill: local is older
	chk2, err := svc.CheckSkill(ctx, "skill.check", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if chk2["status"] != "outdated" {
		t.Fatalf("expected outdated, got %v", chk2["status"])
	}

	// 3. CheckSkill: not found on MCP
	chk3, err := svc.CheckSkill(ctx, "skill.nonexistent", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if chk3["status"] != "missing_on_mcp" {
		t.Fatalf("expected missing_on_mcp, got %v", chk3["status"])
	}

	// 4. CheckLocalSkills
	localList := []map[string]string{
		{"id": "skill.check", "version": "1.2.0"},
	}
	batchRes, err := svc.CheckLocalSkills(ctx, localList)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := batchRes["skills"]; !ok {
		t.Fatalf("expected skills in batchRes: %+v", batchRes)
	}

	// 5. CheckWorkflowSkills
	_, _, _ = svc.UpsertWorkflowYAML(ctx, `id: wf-deps
name: Deps
version: 1.0.0
skills:
  - skill.check
steps:
  - id: s1
    description: d
`, "patch")

	depRes, err := svc.CheckWorkflowSkills(ctx, "wf-deps", map[string]string{
		"skill.check": "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	skillsList, ok := depRes["skills"].([]map[string]any)
	if !ok || len(skillsList) != 1 || skillsList[0]["status"] != "outdated" {
		t.Fatalf("expected outdated in workflow skills: %+v", depRes)
	}
}

func TestGrantManagement(t *testing.T) {
	ctx := context.Background()
	svc := workflowmcp.NewService(workflowmcp.NewMemoryStore())

	_, _, _ = svc.UpsertWorkflowYAML(ctx, `id: wf-g
name: Grant Test
version: 1.0.0
steps:
  - id: s1
    description: d
`, "patch")

	// 1. Grant
	if err := svc.GrantWorkflow(ctx, "EMP-GRANT", "wf-g", "admin-user"); err != nil {
		t.Fatalf("grant failed: %v", err)
	}

	// 2. List Employee Workflows
	empWorkflows, err := svc.ListEmployeeWorkflows(ctx, "EMP-GRANT")
	if err != nil || len(empWorkflows) != 1 || empWorkflows[0].ID != "wf-g" {
		t.Fatalf("list employee workflows failed: %v, wfs=%+v", err, empWorkflows)
	}

	// 3. Store Grants Check
	empGrants, err := svc.Store().ListGrantsByEmployee(ctx, "EMP-GRANT")
	if err != nil || len(empGrants) != 1 || empGrants[0].WorkflowID != "wf-g" {
		t.Fatalf("list by employee failed: %v, grants=%+v", err, empGrants)
	}
	wfGrants, err := svc.Store().ListEmployeesByWorkflow(ctx, "wf-g")
	if err != nil || len(wfGrants) != 1 || wfGrants[0].EmployeeID != "EMP-GRANT" {
		t.Fatalf("list by workflow failed: %v, grants=%+v", err, wfGrants)
	}

	// 4. Revoke
	if err := svc.RevokeWorkflow(ctx, "EMP-GRANT", "wf-g"); err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	afterRevoke, err := svc.ListEmployeeWorkflows(ctx, "EMP-GRANT")
	if err != nil || len(afterRevoke) != 0 {
		t.Fatalf("expected 0 workflows after revoke, got %d", len(afterRevoke))
	}
}

