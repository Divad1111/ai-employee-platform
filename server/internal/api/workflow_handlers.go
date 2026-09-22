package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

// ---------- Workflows ----------

func (d Deps) handleListWorkflows(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.WorkflowMCP == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := d.WorkflowMCP.ListWorkflows(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleGetWorkflow(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	wf, err := d.WorkflowMCP.GetWorkflow(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	yamlOut, _ := workflowmcp.WorkflowToYAML(wf)
	writeJSON(w, http.StatusOK, map[string]any{"workflow": wf, "yaml": yamlOut})
}

func (d Deps) handleUpsertWorkflow(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		YAML   string `json:"yaml"`
		Bump   string `json:"bump"`
		Content string `json:"content"` // 兼容
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON")
		return
	}
	content := body.YAML
	if content == "" {
		content = body.Content
	}
	if content == "" {
		writeErr(w, http.StatusBadRequest, "缺少 yaml")
		return
	}
	wf, action, err := d.WorkflowMCP.UpsertWorkflowYAML(r.Context(), content, body.Bump)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusOK
	if action == "created" {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"status": "ok", "action": action, "workflow": wf})
}

func (d Deps) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := d.WorkflowMCP.DeleteWorkflow(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Skills ----------

func (d Deps) handleListSkillPackages(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	items, err := d.WorkflowMCP.ListSkills(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleGetSkillPackage(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	sp, err := d.WorkflowMCP.GetSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	md, _ := workflowmcp.RenderSkillMD(sp)
	writeJSON(w, http.StatusOK, map[string]any{"skill": sp, "skill_md": md})
}

func (d Deps) handleExportSkillPackage(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	sp, md, err := d.WorkflowMCP.ExportSkill(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skill": sp, "skill_md": md})
}

func (d Deps) handleUpsertSkillPackage(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		SkillMD string                   `json:"skill_md"`
		Content string                   `json:"content"`
		Files   []workflowmcp.SkillFile  `json:"files"`
		Bump    string                   `json:"bump"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON")
		return
	}
	md := body.SkillMD
	if md == "" {
		md = body.Content
	}
	sp, action, err := d.WorkflowMCP.UpsertSkillMD(r.Context(), md, body.Files, body.Bump)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusOK
	if action == "created" {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"status": "ok", "action": action, "skill": sp})
}

func (d Deps) handleDeleteSkillPackage(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := d.WorkflowMCP.DeleteSkill(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Knowledge ----------

func (d Deps) handleListKnowledgeDocs(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	ns := r.URL.Query().Get("namespace")
	items, err := d.WorkflowMCP.ListKnowledge(r.Context(), ns)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleGetKnowledgeDoc(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	doc, err := d.WorkflowMCP.GetKnowledge(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"doc": doc})
}

func (d Deps) handleUpsertKnowledgeDoc(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		Bump    string `json:"bump"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON")
		return
	}
	if body.Path == "" || body.Content == "" {
		writeErr(w, http.StatusBadRequest, "缺少 path 或 content")
		return
	}
	doc, action, err := d.WorkflowMCP.UpsertKnowledgeMD(r.Context(), body.Path, body.Content, body.Bump)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusOK
	if action == "created" {
		code = http.StatusCreated
	}
	writeJSON(w, code, map[string]any{"status": "ok", "action": action, "doc": doc})
}

func (d Deps) handleDeleteKnowledgeDoc(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := d.WorkflowMCP.DeleteKnowledge(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleSearchKnowledgeDocs(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	q := r.URL.Query().Get("q")
	ns := r.URL.Query().Get("namespace")
	hits, err := d.WorkflowMCP.SearchKnowledge(r.Context(), q, ns, 20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": hits})
}

func (d Deps) handleReindexKnowledge(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	n, err := d.WorkflowMCP.ReindexKnowledge(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "reindexed": n})
}

func (d Deps) handleUnifiedSearch(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	q := r.URL.Query().Get("q")
	wf, _ := d.WorkflowMCP.SearchWorkflows(r.Context(), q, 10)
	sk, _ := d.WorkflowMCP.SearchSkills(r.Context(), q, 10)
	kn, _ := d.WorkflowMCP.SearchKnowledge(r.Context(), q, "", 10)
	writeJSON(w, http.StatusOK, map[string]any{
		"workflows": wf, "skills": sk, "knowledge": kn,
	})
}

// ---------- Employee grants & MCP tokens ----------

func (d Deps) handleListEmployeeWorkflows(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	id := r.PathValue("id")
	items, err := d.WorkflowMCP.ListEmployeeWorkflows(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	eff, _ := d.WorkflowMCP.EffectiveSkills(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"workflows": items, "effective_skills": eff})
}

func (d Deps) handleGrantEmployeeWorkflow(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		WorkflowID string `json:"workflow_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WorkflowID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 workflow_id")
		return
	}
	grantedBy := ""
	if sess != nil {
		grantedBy = sess.UserID
	}
	if err := d.WorkflowMCP.GrantWorkflow(r.Context(), r.PathValue("id"), body.WorkflowID, grantedBy); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (d Deps) handleRevokeEmployeeWorkflow(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	wfID := r.URL.Query().Get("workflow_id")
	if wfID == "" {
		writeErr(w, http.StatusBadRequest, "缺少 workflow_id")
		return
	}
	if err := d.WorkflowMCP.RevokeWorkflow(r.Context(), r.PathValue("id"), wfID); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleListEmployeeMCPTokens(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.MCPAuth == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	items, err := d.MCPAuth.ListBySubject(r.Context(), mcpauth.SubjectEmployee, r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleIssueEmployeeMCPToken(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.MCPAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP Auth 未启用")
		return
	}
	var body struct {
		Label     string `json:"label"`
		ExpiresIn int64  `json:"expires_in_sec"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	createdBy := ""
	if sess != nil {
		createdBy = sess.UserID
	}
	var exp time.Duration
	if body.ExpiresIn > 0 {
		exp = time.Duration(body.ExpiresIn) * time.Second
	}
	res, err := d.MCPAuth.Issue(r.Context(), mcpauth.SubjectEmployee, r.PathValue("id"),
		mcpauth.ScopeRead, body.Label, createdBy, exp)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"token": res.Token, "secret": res.Secret,
		"mcp_json_hint": map[string]any{
			"url":     "/mcp",
			"headers": map[string]string{"Authorization": "Bearer " + res.Secret},
		},
	})
}

func (d Deps) handleRevokeMCPToken(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := d.MCPAuth.Revoke(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------- Import from PersonalWorkMCP data zip ----------

func (d Deps) handleImportWorkflowMCP(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "需要 multipart zip")
		return
	}
	f, _, err := r.FormFile("file")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "缺少 file")
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效 zip")
		return
	}
	stats := map[string]int{"workflows": 0, "skills": 0, "knowledge": 0, "errors": 0}
	// 收集技能包目录
	skillDirs := map[string]map[string][]byte{} // cursor_name -> path -> content
	for _, zf := range zr.File {
		name := strings.ReplaceAll(zf.Name, "\\", "/")
		// 去掉可能的 data/ 前缀
		name = strings.TrimPrefix(name, "data/")
		if strings.HasPrefix(name, "workflows/") && strings.HasSuffix(name, ".yaml") {
			rc, err := zf.Open()
			if err != nil {
				stats["errors"]++
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if _, _, err := d.WorkflowMCP.UpsertWorkflowYAML(r.Context(), string(b), "patch"); err != nil {
				stats["errors"]++
			} else {
				stats["workflows"]++
			}
			continue
		}
		if strings.HasPrefix(name, "skills/") {
			rest := strings.TrimPrefix(name, "skills/")
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) < 2 {
				continue
			}
			dir, rel := parts[0], parts[1]
			if skillDirs[dir] == nil {
				skillDirs[dir] = map[string][]byte{}
			}
			rc, err := zf.Open()
			if err != nil {
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			skillDirs[dir][rel] = b
			continue
		}
		if strings.HasPrefix(name, "knowledge/") && strings.HasSuffix(name, ".md") {
			rc, err := zf.Open()
			if err != nil {
				stats["errors"]++
				continue
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			rel := strings.TrimPrefix(name, "knowledge/")
			if _, _, err := d.WorkflowMCP.UpsertKnowledgeMD(r.Context(), rel, string(b), "patch"); err != nil {
				stats["errors"]++
			} else {
				stats["knowledge"]++
			}
		}
	}
	for _, files := range skillDirs {
		mdBytes, ok := files["SKILL.md"]
		if !ok {
			stats["errors"]++
			continue
		}
		var skillFiles []workflowmcp.SkillFile
		for p, content := range files {
			if p == "SKILL.md" {
				continue
			}
			skillFiles = append(skillFiles, workflowmcp.SkillFile{Path: p, Content: content})
		}
		if _, _, err := d.WorkflowMCP.UpsertSkillMD(r.Context(), string(mdBytes), skillFiles, "patch"); err != nil {
			stats["errors"]++
		} else {
			stats["skills"]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "stats": stats})
}

// handleSyncSkillsToWorkstation 触发技能包同步命令（需 Scheduler/Pusher）。
func (d Deps) handleSyncSkillsToWorkstation(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		WorkstationID string   `json:"workstation_id"`
		EmployeeID    string   `json:"employee_id"`
		SkillIDs      []string `json:"skill_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.WorkstationID == "" {
		writeErr(w, http.StatusBadRequest, "需要 workstation_id")
		return
	}
	if d.SkillSyncer == nil {
		writeErr(w, http.StatusServiceUnavailable, "技能同步未启用")
		return
	}
	if err := d.SkillSyncer.SyncSkills(r.Context(), body.WorkstationID, body.EmployeeID, body.SkillIDs); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}
