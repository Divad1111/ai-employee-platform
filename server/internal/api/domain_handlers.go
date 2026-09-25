package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

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
			"cpu_percent": v.CPUPercent, "memory_percent": v.MemoryPercent, "disk_percent": v.DiskPercent,
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
		"my_quota":            d.attachUsageBreakdown(r, sess, d.buildMyQuota(r, sess), jobs),
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type usageBucket struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	Jobs         int    `json:"jobs"`
}

func (d Deps) attachUsageBreakdown(r *http.Request, sess *auth.Session, out map[string]any, jobs []*job.Job) map[string]any {
	if out == nil {
		out = map[string]any{}
	}
	if sess == nil {
		return out
	}
	period, _ := out["period_key"].(string)
	if period == "" {
		period = time.Now().UTC().Format("2006-01")
		out["period_key"] = period
	}
	empName := map[string]string{}
	if d.Employees != nil {
		if list, err := d.Employees.List(r.Context()); err == nil {
			for _, e := range list {
				if e != nil {
					empName[e.ID] = e.Name
				}
			}
		}
	}
	wsName := map[string]string{}
	if d.Workstations != nil {
		for _, w := range d.Workstations.List(r.Context()) {
			wsName[w.ID] = w.Name
		}
	}
	var inputSum, outputSum int64
	byWS := map[string]*usageBucket{}
	byEmp := map[string]*usageBucket{}
	byAgent := map[string]*usageBucket{}
	for _, j := range jobs {
		if j == nil || j.CreatedBy != sess.UserID || j.CreatedAt.UTC().Format("2006-01") != period {
			continue
		}
		if j.InputTokens == 0 && j.OutputTokens == 0 {
			continue
		}
		inputSum += j.InputTokens
		outputSum += j.OutputTokens
		addBucket(byWS, j.WorkstationID, firstNonEmpty(wsName[j.WorkstationID], j.WorkstationID, "未指定工作站"), j)
		addBucket(byEmp, j.EmployeeID, firstNonEmpty(empName[j.EmployeeID], j.EmployeeID, "未指定数字员工"), j)
		agent := j.Agent
		if agent == "" {
			agent = "未识别 Agent"
		}
		addBucket(byAgent, agent, agent, j)
	}
	out["input_tokens"] = inputSum
	out["output_tokens"] = outputSum
	used := inputSum + outputSum
	out["tokens_used"] = used
	if limit, ok := out["token_limit"].(int64); ok && limit > 0 {
		pct := float64(used) / float64(limit) * 100
		if pct > 100 {
			pct = 100
		}
		out["usage_percent"] = pct
		rem := limit - used
		if rem < 0 {
			rem = 0
		}
		out["remaining"] = rem
		out["unlimited"] = false
	}
	out["by_workstation"] = bucketList(byWS)
	out["by_employee"] = bucketList(byEmp)
	out["by_agent"] = bucketList(byAgent)
	return out
}

func addBucket(m map[string]*usageBucket, id, name string, j *job.Job) {
	if id == "" {
		id = name
	}
	b := m[id]
	if b == nil {
		b = &usageBucket{ID: id, Name: name}
		m[id] = b
	}
	b.InputTokens += j.InputTokens
	b.OutputTokens += j.OutputTokens
	b.TotalTokens += j.InputTokens + j.OutputTokens
	b.Jobs++
}

func bucketList(m map[string]*usageBucket) []*usageBucket {
	out := make([]*usageBucket, 0, len(m))
	for _, b := range m {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TotalTokens > out[j].TotalTokens })
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
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
