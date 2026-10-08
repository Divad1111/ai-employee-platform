package api

import (
	"encoding/json"
	"net/http"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/tokenusage"
)

func (d Deps) handleGetJobTokenUsage(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.TokenUsage == nil {
		writeErr(w, http.StatusServiceUnavailable, "token usage 未启用")
		return
	}
	jobID := r.PathValue("id")
	j, err := d.Jobs.Get(r.Context(), jobID)
	if err != nil || j == nil {
		writeErr(w, http.StatusNotFound, "任务不存在")
		return
	}
	if !d.canAccessJob(r, sess, j, "job.read") {
		writeErr(w, http.StatusNotFound, "任务不存在或无权限")
		return
	}
	sum, runs, err := d.TokenUsage.GetJobUsage(r.Context(), jobID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"job_id":  jobID,
		"summary": sum,
		"runs":    runs,
	})
}

func (d Deps) handleUpsertJobTokenUsage(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.TokenUsage == nil {
		writeErr(w, http.StatusServiceUnavailable, "token usage 未启用")
		return
	}
	jobID := r.PathValue("id")
	j, err := d.Jobs.Get(r.Context(), jobID)
	if err != nil || j == nil {
		writeErr(w, http.StatusNotFound, "任务不存在")
		return
	}
	if !d.canAccessJob(r, sess, j, "job.write") {
		writeErr(w, http.StatusNotFound, "任务不存在或无权限")
		return
	}
	var in tokenusage.UpsertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	in.JobID = jobID
	if in.DigitalEmployeeID == "" {
		in.DigitalEmployeeID = j.EmployeeID
	}
	if in.WorkstationID == "" {
		in.WorkstationID = j.WorkstationID
	}
	saved, sum, created, err := d.TokenUsage.Upsert(r.Context(), in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "job.token_usage.upsert", "success", clientIP(r), map[string]string{
			"job_id": jobID, "usage_id": saved.ID, "provider_run_id": saved.ProviderRunID,
		})
	}
	writeJSON(w, code, map[string]any{
		"success":  true,
		"usage_id": saved.ID,
		"created":  created,
		"summary":  sum,
		"run":      saved,
	})
}
