package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/automation"
)

func (d Deps) handleListAutomations(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	typ := r.URL.Query().Get("type")
	list, err := d.Automation.List(r.Context(), typ)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !d.scopeAll(r, sess, "automation.read") {
		out := list[:0]
		for _, a := range list {
			if a != nil && sess != nil && a.CreatedBy == sess.UserID {
				out = append(out, a)
			}
		}
		list = out
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleCreateAutomation(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	var in automation.CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON")
		return
	}
	a, secrets, err := d.Automation.Create(r.Context(), in, sess.UserID)
	if err != nil {
		if errors.Is(err, automation.ErrInvalidInput) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := map[string]any{"item": a}
	if len(secrets) > 0 {
		out["secrets"] = secrets // 仅创建时返回一次
	}
	writeJSON(w, http.StatusCreated, out)
}

func (d Deps) handleGetAutomation(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	a, err := d.Automation.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if !d.scopeAll(r, sess, "automation.read") && (sess == nil || a.CreatedBy != sess.UserID) {
		writeErr(w, http.StatusNotFound, "automation 不存在")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (d Deps) handleUpdateAutomation(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	var in automation.UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON")
		return
	}
	cur, err := d.Automation.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if !d.scopeAll(r, sess, "automation.write") && cur.CreatedBy != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权修改该自动化")
		return
	}
	a, err := d.Automation.Update(r.Context(), r.PathValue("id"), in, sess.UserID)
	if err != nil {
		if errors.Is(err, automation.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, automation.ErrInvalidInput) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (d Deps) handleDeleteAutomation(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	cur, err := d.Automation.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if !d.scopeAll(r, sess, "automation.write") && cur.CreatedBy != sess.UserID {
		writeErr(w, http.StatusForbidden, "无权删除该自动化")
		return
	}
	if err := d.Automation.Delete(r.Context(), r.PathValue("id"), sess.UserID); err != nil {
		if errors.Is(err, automation.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (d Deps) handleListCalendarItems(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	if !d.allowAutomation(w, r, sess, r.PathValue("id"), "automation.read") {
		return
	}
	runDate := r.URL.Query().Get("date")
	items, err := d.Automation.ListCalendarItems(r.Context(), r.PathValue("id"), runDate)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handlePutCalendarItems(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	if !d.allowAutomation(w, r, sess, r.PathValue("id"), "automation.write") {
		return
	}
	var body struct {
		Date     string                         `json:"date"`
		RunClock string                         `json:"run_clock"`
		Items    []automation.CalendarItemInput `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Date == "" {
		writeErr(w, http.StatusBadRequest, "需要 date 与 items")
		return
	}
	if body.RunClock != "" {
		for i := range body.Items {
			body.Items[i].RunClock = body.RunClock
		}
	}
	items, err := d.Automation.ReplaceCalendarItems(r.Context(), r.PathValue("id"), body.Date, body.Items, sess.UserID)
	if err != nil {
		if errors.Is(err, automation.ErrInvalidInput) || errors.Is(err, automation.ErrNotFound) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) handleListAutomationRuns(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	if !d.allowAutomation(w, r, sess, r.PathValue("id"), "automation.read") {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := d.Automation.ListRuns(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": runs})
}

func (d Deps) handleRotateAutomationSecrets(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	secrets, err := d.Automation.RotateWebhookSecrets(r.Context(), r.PathValue("id"), sess.UserID)
	if err != nil {
		if errors.Is(err, automation.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": secrets})
}

func (d Deps) handleAutomationTick(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	d.Automation.Tick(r.Context(), time.Now().UTC())
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (d Deps) handleAutomationWebhook(w http.ResponseWriter, r *http.Request) {
	if d.Automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "automation 未启用")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取 body 失败")
		return
	}
	run, j, err := d.Automation.HandleWebhook(r.Context(), r.PathValue("path_token"), r, body)
	if err != nil {
		switch {
		case errors.Is(err, automation.ErrNotFound):
			writeErr(w, http.StatusNotFound, "webhook 不存在")
		case errors.Is(err, automation.ErrRateLimited):
			writeErr(w, http.StatusTooManyRequests, "限流")
		case errors.Is(err, automation.ErrForbiddenWebhook):
			writeErr(w, http.StatusUnauthorized, "校验失败")
		default:
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	out := map[string]any{"run": run}
	if j != nil {
		out["job_id"] = j.ID
	}
	writeJSON(w, http.StatusAccepted, out)
}

func (d Deps) allowAutomation(w http.ResponseWriter, r *http.Request, sess *auth.Session, id, perm string) bool {
	a, err := d.Automation.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "automation 不存在")
		return false
	}
	if !d.scopeAll(r, sess, perm) && (sess == nil || a.CreatedBy != sess.UserID) {
		writeErr(w, http.StatusForbidden, "无权访问该自动化")
		return false
	}
	return true
}
