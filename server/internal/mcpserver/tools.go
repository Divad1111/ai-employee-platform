package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

func toolDefs() []map[string]any {
	str := map[string]any{"type": "string"}
	num := map[string]any{"type": "number"}
	obj := func(props map[string]any, req ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(req) > 0 {
			m["required"] = req
		}
		return m
	}
	tool := func(name, desc string, schema map[string]any) map[string]any {
		return map[string]any{"name": name, "description": desc, "inputSchema": schema}
	}
	return []map[string]any{
		tool("list_workflows", "列出工作流", obj(map[string]any{})),
		tool("get_workflow", "获取工作流详情", obj(map[string]any{"id": str}, "id")),
		tool("search_workflows", "搜索工作流", obj(map[string]any{"query": str, "limit": num}, "query")),
		tool("upsert_workflow", "创建或更新工作流（需写权限）", obj(map[string]any{"content": str, "bump": str}, "content")),
		tool("delete_workflow", "删除工作流（需写权限）", obj(map[string]any{"id": str}, "id")),

		tool("list_skills", "列出技能包", obj(map[string]any{})),
		tool("get_skill", "获取技能包（文件索引）", obj(map[string]any{"id": str}, "id")),
		tool("search_skills", "搜索技能", obj(map[string]any{"query": str, "limit": num}, "query")),
		tool("export_skill", "导出完整技能包", obj(map[string]any{"id": str}, "id")),
		tool("check_skill", "对比本地与 MCP 技能版本", obj(map[string]any{"id": str, "local_version": str}, "id")),
		tool("check_local_skills", "根据客户端上报的本地技能清单对比", obj(map[string]any{"local_skills": map[string]any{"type": "array"}})),
		tool("check_workflow_skills", "检查工作流依赖技能", obj(map[string]any{"workflow_id": str, "local_skills": map[string]any{"type": "object"}}, "workflow_id")),
		tool("upsert_skill", "创建或更新技能包（需写权限）", obj(map[string]any{"content": str, "files": map[string]any{"type": "array"}, "bump": str}, "content")),
		tool("delete_skill", "删除技能包（需写权限）", obj(map[string]any{"id": str}, "id")),
		tool("sync_skill", "触发技能同步到工作站（需写权限）", obj(map[string]any{"id": str, "workstation_id": str, "employee_id": str}, "id", "workstation_id")),
		tool("sync_all_skills", "触发全部技能同步到工作站（需写权限）", obj(map[string]any{"workstation_id": str, "employee_id": str}, "workstation_id")),

		tool("search_knowledge", "检索知识库", obj(map[string]any{"query": str, "limit": num, "namespace": str}, "query")),
		tool("get_knowledge", "获取知识文档", obj(map[string]any{"id": str}, "id")),
		tool("search_cases", "检索 cases 命名空间", obj(map[string]any{"query": str, "limit": num}, "query")),
		tool("upsert_knowledge", "创建或更新知识（需写权限）", obj(map[string]any{"path": str, "content": str, "bump": str}, "path", "content")),
		tool("delete_knowledge", "删除知识（需写权限）", obj(map[string]any{"id": str}, "id")),

		tool("reload_resources", "重建知识检索索引", obj(map[string]any{})),
	}
}

func (s *Server) callTool(ctx context.Context, name string, args map[string]any) (any, error) {
	if s.WF == nil {
		return nil, fmt.Errorf("工作流MCP 未启用")
	}
	caller := CallerFrom(ctx)
	needWrite := map[string]bool{
		"upsert_workflow": true, "delete_workflow": true,
		"upsert_skill": true, "delete_skill": true,
		"upsert_knowledge": true, "delete_knowledge": true,
		"sync_skill": true, "sync_all_skills": true,
	}
	if needWrite[name] && (caller == nil || !caller.CanWrite) {
		return nil, fmt.Errorf("需要写权限")
	}

	str := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		return ""
	}
	num := func(k string, def int) int {
		switch v := args[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			n, _ := strconv.Atoi(v)
			if n > 0 {
				return n
			}
		}
		return def
	}

	switch name {
	case "list_workflows":
		list, err := s.WF.ListWorkflows(ctx)
		if err != nil {
			return nil, err
		}
		return filterWorkflows(caller, list), nil
	case "get_workflow":
		id := str("id")
		if caller != nil && caller.EmployeeScope != nil && !caller.EmployeeScope.InScopeWorkflow(id) {
			return nil, fmt.Errorf("未授权访问该工作流")
		}
		return s.WF.GetWorkflow(ctx, id)
	case "search_workflows":
		hits, err := s.WF.SearchWorkflows(ctx, str("query"), num("limit", 10))
		if err != nil {
			return nil, err
		}
		return filterSearchHits(caller, hits), nil
	case "upsert_workflow":
		wf, action, err := s.WF.UpsertWorkflowYAML(ctx, str("content"), str("bump"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": action, "id": wf.ID, "version": wf.Version}, nil
	case "delete_workflow":
		if err := s.WF.DeleteWorkflow(ctx, str("id")); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": "deleted", "id": str("id")}, nil

	case "list_skills":
		list, err := s.WF.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		return filterSkills(caller, list), nil
	case "get_skill":
		id := str("id")
		if caller != nil && caller.EmployeeScope != nil && !caller.EmployeeScope.InScopeSkill(id) {
			return nil, fmt.Errorf("未授权访问该技能")
		}
		return s.WF.GetSkill(ctx, id)
	case "search_skills":
		hits, err := s.WF.SearchSkills(ctx, str("query"), num("limit", 10))
		if err != nil {
			return nil, err
		}
		return filterSkillHits(caller, hits), nil
	case "export_skill":
		id := str("id")
		if caller != nil && caller.EmployeeScope != nil && !caller.EmployeeScope.InScopeSkill(id) {
			return nil, fmt.Errorf("未授权访问该技能")
		}
		sp, md, err := s.WF.ExportSkill(ctx, id)
		if err != nil {
			return nil, err
		}
		return map[string]any{"skill": sp, "skill_md": md}, nil
	case "check_skill":
		return s.WF.CheckSkill(ctx, str("id"), str("local_version"))
	case "check_local_skills":
		var local []map[string]string
		if raw, ok := args["local_skills"]; ok {
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &local)
		}
		return s.WF.CheckLocalSkills(ctx, local)
	case "check_workflow_skills":
		local := map[string]string{}
		if raw, ok := args["local_skills"].(map[string]any); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok {
					local[k] = s
				}
			}
		}
		return s.WF.CheckWorkflowSkills(ctx, str("workflow_id"), local)
	case "upsert_skill":
		var files []workflowmcp.SkillFile
		if raw, ok := args["files"]; ok {
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &files)
		}
		sp, action, err := s.WF.UpsertSkillMD(ctx, str("content"), files, str("bump"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": action, "id": sp.ID, "version": sp.Version, "cursor_name": sp.CursorName}, nil
	case "delete_skill":
		if err := s.WF.DeleteSkill(ctx, str("id")); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": "deleted", "id": str("id")}, nil
	case "sync_skill":
		if s.Syncer == nil {
			return nil, fmt.Errorf("技能同步未启用")
		}
		if err := s.Syncer.SyncSkills(ctx, str("workstation_id"), str("employee_id"), []string{str("id")}); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": "synced"}, nil
	case "sync_all_skills":
		if s.Syncer == nil {
			return nil, fmt.Errorf("技能同步未启用")
		}
		list, err := s.WF.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		ids := make([]string, 0, len(list))
		for _, sp := range list {
			ids = append(ids, sp.ID)
		}
		if err := s.Syncer.SyncSkills(ctx, str("workstation_id"), str("employee_id"), ids); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": "synced", "count": len(ids)}, nil

	case "search_knowledge":
		hits, err := s.WF.SearchKnowledge(ctx, str("query"), str("namespace"), num("limit", 10))
		if err != nil {
			return nil, err
		}
		return filterKnowledge(caller, hits), nil
	case "get_knowledge":
		id := str("id")
		doc, err := s.WF.GetKnowledge(ctx, id)
		if err != nil {
			return nil, err
		}
		if caller != nil && caller.EmployeeScope != nil && !caller.EmployeeScope.InScopeKnowledge(doc.Path) && !caller.EmployeeScope.InScopeKnowledge(doc.ID) {
			return nil, fmt.Errorf("未授权访问该知识")
		}
		return doc, nil
	case "search_cases":
		hits, err := s.WF.SearchCases(ctx, str("query"), num("limit", 10))
		if err != nil {
			return nil, err
		}
		return filterKnowledge(caller, hits), nil
	case "upsert_knowledge":
		doc, action, err := s.WF.UpsertKnowledgeMD(ctx, str("path"), str("content"), str("bump"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": action, "id": doc.ID, "path": doc.Path, "version": doc.Version}, nil
	case "delete_knowledge":
		if err := s.WF.DeleteKnowledge(ctx, str("id")); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "action": "deleted", "id": str("id")}, nil
	case "reload_resources":
		n, err := s.WF.ReindexKnowledge(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "message": "Resources reloaded", "reindexed": n}, nil
	default:
		return nil, fmt.Errorf("未知 tool: %s", name)
	}
}

func filterWorkflows(c *Caller, list []*workflowmcp.Workflow) []*workflowmcp.Workflow {
	if c == nil || c.EmployeeScope == nil {
		return list
	}
	var out []*workflowmcp.Workflow
	for _, w := range list {
		if c.EmployeeScope.InScopeWorkflow(w.ID) {
			out = append(out, w)
		}
	}
	return out
}

func filterSkills(c *Caller, list []*workflowmcp.SkillPackage) []*workflowmcp.SkillPackage {
	if c == nil || c.EmployeeScope == nil {
		return list
	}
	var out []*workflowmcp.SkillPackage
	for _, sp := range list {
		if c.EmployeeScope.InScopeSkill(sp.ID) || c.EmployeeScope.InScopeSkill(sp.CursorName) {
			out = append(out, sp)
		}
	}
	return out
}

func filterSearchHits(c *Caller, hits []workflowmcp.SearchHit) []workflowmcp.SearchHit {
	if c == nil || c.EmployeeScope == nil {
		return hits
	}
	var out []workflowmcp.SearchHit
	for _, h := range hits {
		if c.EmployeeScope.InScopeWorkflow(h.ID) {
			out = append(out, h)
		}
	}
	return out
}

func filterSkillHits(c *Caller, hits []workflowmcp.SearchHit) []workflowmcp.SearchHit {
	if c == nil || c.EmployeeScope == nil {
		return hits
	}
	var out []workflowmcp.SearchHit
	for _, h := range hits {
		if c.EmployeeScope.InScopeSkill(h.ID) || c.EmployeeScope.InScopeSkill(h.CursorName) {
			out = append(out, h)
		}
	}
	return out
}

func filterKnowledge(c *Caller, hits []workflowmcp.KnowledgeHit) []workflowmcp.KnowledgeHit {
	if c == nil || c.EmployeeScope == nil {
		return hits
	}
	var out []workflowmcp.KnowledgeHit
	for _, h := range hits {
		if c.EmployeeScope.InScopeKnowledge(h.Path) || c.EmployeeScope.InScopeKnowledge(h.ID) {
			out = append(out, h)
		}
	}
	return out
}
