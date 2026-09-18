package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/permission"
)

// handleDashboard 对齐设计文档 §9：统计 + 活跃 Job + Workstation 资源。
func (d Deps) handleDashboard(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	emps, _ := d.Employees.List(r.Context())
	jobs, _ := d.Jobs.List(r.Context())
	active, _ := d.Jobs.ActiveCount(r.Context())
	wss := d.Workstations.List(r.Context())
	online := 0
	errors := 0
	busy := 0
	for _, v := range wss {
		if v.Status == "ONLINE" {
			online++
		}
		if v.CertStatus == "REVOKED" {
			errors++
		}
	}
	activeJobs := make([]any, 0)
	for _, j := range jobs {
		st := string(j.Status)
		if st == "SUCCESS" || st == "FAILED" || st == "CANCELLED" || st == "TIMEOUT" {
			continue
		}
		if st == "RUNNING" || st == "STARTING" || st == "WAITING_APPROVAL" {
			busy++
		}
		activeJobs = append(activeJobs, map[string]any{
			"id": j.ID, "employee_id": j.EmployeeID, "status": st, "prompt": truncate(j.Prompt, 80),
		})
		if len(activeJobs) >= 20 {
			break
		}
	}
	wsRows := make([]any, 0, len(wss))
	for _, v := range wss {
		wsRows = append(wsRows, map[string]any{
			"id": v.ID, "name": v.Name, "status": v.Status,
			"cpu_percent": v.CPUPercent, "memory_percent": v.MemoryPercent,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"employees":           len(emps),
		"workstations":        len(wss),
		"workstations_online": online,
		"jobs":                len(jobs),
		"active_jobs":         active,
		"busy":                busy,
		"errors":              errors,
		"recent_active_jobs":  activeJobs,
		"workstations_detail": wsRows,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// handleEmployeeOverview 对齐 §10：详情聚合。
func (d Deps) handleEmployeeOverview(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	id := r.PathValue("id")
	e, err := d.Employees.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	sessions := make([]any, 0)
	if d.Sessions != nil {
		all, _ := d.Sessions.List(r.Context())
		for _, s := range all {
			if s.EmployeeID == id {
				sessions = append(sessions, s)
			}
		}
	}
	jobsOut := make([]any, 0)
	if d.Jobs != nil {
		all, _ := d.Jobs.List(r.Context())
		for _, j := range all {
			if j.EmployeeID == id {
				jobsOut = append(jobsOut, j)
			}
		}
	}
	var skills any
	if d.Skills != nil {
		skills = d.Skills.ListByEmployee(r.Context(), id)
	}
	var knowledge any
	if d.Knowledge != nil {
		knowledge = d.Knowledge.ListByEmployee(r.Context(), id)
	}
	var binding any
	if d.Feishu != nil {
		binding = d.Feishu.BindingByEmployee(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"employee": e, "sessions": sessions, "jobs": jobsOut,
		"skills": skills, "knowledge": knowledge, "feishu": binding,
	})
}

func (d Deps) handleListSkills(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Skills == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Skills.List(r.Context())})
}

func (d Deps) handleCreateSkill(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Skills == nil {
		writeErr(w, http.StatusServiceUnavailable, "skills 未启用")
		return
	}
	var body struct {
		Name, Description, Category string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	sk, err := d.Skills.Create(r.Context(), body.Name, body.Description, body.Category)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sk)
}

func (d Deps) handleUpdateSkill(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		Name, Description, Category string
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	sk, err := d.Skills.Update(r.Context(), r.PathValue("id"), body.Name, body.Description, body.Category)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sk)
}

func (d Deps) handleDeleteSkill(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := d.Skills.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleBindSkill(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		EmployeeID string `json:"employee_id"`
		SkillID    string `json:"skill_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	if err := d.Skills.Bind(r.Context(), body.EmployeeID, body.SkillID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound"})
}

func (d Deps) handleListKnowledge(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Knowledge == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []any{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Knowledge.List(r.Context())})
}

func (d Deps) handleCreateKnowledge(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		Title, Summary, Content string
		Tags                    []string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	e, err := d.Knowledge.Create(r.Context(), body.Title, body.Summary, body.Content, body.Tags)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (d Deps) handleUpdateKnowledge(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		Title, Summary, Content string
		Tags                    []string
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	e, err := d.Knowledge.Update(r.Context(), r.PathValue("id"), body.Title, body.Summary, body.Content, body.Tags)
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (d Deps) handleDeleteKnowledge(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if err := d.Knowledge.Delete(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (d Deps) handleBindKnowledge(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	var body struct {
		EmployeeID  string `json:"employee_id"`
		KnowledgeID string `json:"knowledge_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	if err := d.Knowledge.Bind(r.Context(), body.EmployeeID, body.KnowledgeID); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "bound"})
}

func (d Deps) handleUpsertPermissionProfile(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var p permission.Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.ID == "" || p.Name == "" {
		writeErr(w, http.StatusBadRequest, "需要 id / name")
		return
	}
	if err := d.PermissionStore.SaveProfile(r.Context(), &p); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	d.Audit.Log(r.Context(), "USER", sess.UserID, "permission.profile.upsert", "success", clientIP(r), map[string]string{"id": p.ID})
	writeJSON(w, http.StatusOK, p)
}

func (d Deps) handleUpsertPermissionRule(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var rule permission.Rule
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil || rule.ProfileID == "" || rule.Action == "" {
		writeErr(w, http.StatusBadRequest, "需要 profile_id / action")
		return
	}
	eff := strings.ToUpper(rule.Effect)
	if eff != permission.EffectAllow && eff != permission.EffectAsk && eff != permission.EffectDeny {
		writeErr(w, http.StatusBadRequest, "effect 必须为 ALLOW/ASK/DENY")
		return
	}
	rule.Effect = eff
	if rule.ID == "" {
		rule.ID = rule.ProfileID + ":" + rule.Action
	}
	if err := d.PermissionStore.UpsertRule(r.Context(), &rule); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	d.Audit.Log(r.Context(), "USER", sess.UserID, "permission.rule.upsert", "success", clientIP(r), map[string]string{
		"profile_id": rule.ProfileID, "action": rule.Action, "effect": rule.Effect,
	})
	writeJSON(w, http.StatusOK, rule)
}

func (d Deps) handleListFeishuBindings(w http.ResponseWriter, _ *http.Request, _ *auth.Session) {
	if d.Feishu == nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": []feishu.Binding{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": d.Feishu.ListBindings()})
}
