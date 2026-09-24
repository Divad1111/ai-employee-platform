package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/authz"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/permission"
)

// handleDashboard 对齐设计文档 §9：统计 + 活跃 Job + Workstation 资源（按当前用户 Scope 过滤）。
func (d Deps) handleDashboard(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	allEmps, _ := d.Employees.List(r.Context())
	emps := d.filterEmployees(r, sess, allEmps)
	empIDs := map[string]struct{}{}
	for _, e := range emps {
		empIDs[e.ID] = struct{}{}
	}

	jobScope := d.resolveScope(r.Context(), sess, "job.read")
	allJobs, _ := d.Jobs.List(r.Context())
	jobs := make([]*job.Job, 0, len(allJobs))
	for _, j := range allJobs {
		if jobScope == authz.ScopeALL {
			jobs = append(jobs, j)
			continue
		}
		if _, ok := empIDs[j.EmployeeID]; ok {
			jobs = append(jobs, j)
		}
	}

	allWS := d.Workstations.List(r.Context())
	ids := make([]string, 0, len(allWS))
	for _, v := range allWS {
		ids = append(ids, v.ID)
	}
	allowedWS := d.filterWorkstationIDs(r, sess, ids)
	allowedSet := map[string]struct{}{}
	for _, id := range allowedWS {
		allowedSet[id] = struct{}{}
	}
	wsScope := d.resolveScope(r.Context(), sess, "workstation.read")

	online := 0
	wsRows := make([]any, 0)
	for _, v := range allWS {
		if wsScope != authz.ScopeALL {
			if _, ok := allowedSet[v.ID]; !ok {
				continue
			}
		}
		if v.Status == "ONLINE" {
			online++
		}
		wsRows = append(wsRows, map[string]any{
			"id": v.ID, "name": v.Name, "status": v.Status,
			"cpu_percent": v.CPUPercent, "memory_percent": v.MemoryPercent,
		})
	}

	errors := 0
	busy := 0
	active := 0
	activeJobs := make([]any, 0)
	for _, j := range jobs {
		st := string(j.Status)
		if st == job.StatusFailed || st == job.StatusTimeout || st == job.StatusUnknown {
			errors++
		}
		if st == job.StatusRunning || st == job.StatusStarting || st == job.StatusWaitingApproval {
			busy++
		}
		if st != job.StatusSuccess && st != job.StatusFailed && st != job.StatusCancelled && st != job.StatusTimeout {
			active++
			if len(activeJobs) < 20 {
				activeJobs = append(activeJobs, map[string]any{
					"id": j.ID, "employee_id": j.EmployeeID, "status": st, "prompt": truncate(j.Prompt, 80),
				})
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"employees":           len(emps),
		"workstations":        len(wsRows),
		"workstations_online": online,
		"jobs":                len(jobs),
		"active_jobs":         active,
		"busy":                busy,
		"errors":              errors,
		"recent_active_jobs":  activeJobs,
		"workstations_detail": wsRows,
		"my_quota":            d.buildMyQuota(r, sess),
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
	var workflows any
	var effectiveSkills any
	if d.WorkflowMCP != nil {
		workflows, _ = d.WorkflowMCP.ListEmployeeWorkflows(r.Context(), id)
		effectiveSkills, _ = d.WorkflowMCP.EffectiveSkills(r.Context(), id)
	}
	var binding any
	if d.Feishu != nil {
		binding = d.Feishu.BindingByEmployee(id)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"employee": e, "sessions": sessions, "jobs": jobsOut,
		"workflows": workflows, "effective_skills": effectiveSkills, "feishu": binding,
	})
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
