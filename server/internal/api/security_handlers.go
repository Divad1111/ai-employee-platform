package api

import (
	"net/http"
	"time"

	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/permission"
)

func (d Deps) handleListPermissionProfiles(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Permission == nil {
		writeErr(w, http.StatusServiceUnavailable, "permission 未启用")
		return
	}
	list, err := d.PermissionStore.ListProfiles(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleListPermissionRules(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	profileID := r.URL.Query().Get("profile_id")
	rules, err := d.Permission.ExportRules(r.Context(), profileID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": rules})
}

func (d Deps) handleEvaluatePermission(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var req permission.Request
	if err := decodeJSON(r, &req); err != nil || req.Action == "" {
		writeErr(w, http.StatusBadRequest, "需要 action")
		return
	}
	if req.ActorID == "" {
		req.ActorID = sess.UserID
		req.ActorType = "USER"
	}
	if d.Approvals != nil {
		dcs, ar, err := d.Approvals.EvaluateAndMaybeCreate(r.Context(), req, sess.UserID)
		out := map[string]any{"decision": dcs}
		if ar != nil {
			out["approval"] = ar
		}
		if err != nil && err != approval.ErrTOTPNotConfigured {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err == approval.ErrTOTPNotConfigured {
			out["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": d.Permission.Decide(r.Context(), req)})
}

func (d Deps) handleListApprovals(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	list, err := d.Approvals.List(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": list})
}

func (d Deps) handleGetApproval(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	ar, err := d.Approvals.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ar)
}

func (d Deps) handleApprove(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		TOTP string `json:"totp"`
	}
	_ = decodeJSON(r, &body)
	ar, err := d.Approvals.Approve(r.Context(), r.PathValue("id"), sess.UserID, body.TOTP, clientIP(r))
	if err != nil {
		code := http.StatusBadRequest
		if err == approval.ErrTOTPNotConfigured || err == approval.ErrTOTPRequired {
			code = http.StatusForbidden
		}
		writeErr(w, code, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ar)
}

func (d Deps) handleRejectApproval(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	ar, err := d.Approvals.Reject(r.Context(), r.PathValue("id"), sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ar)
}

func (d Deps) handleEnrollTOTP(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	sec, b, err := d.Approvals.EnrollTOTP(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 明文 secret 仅此一次返回
	writeJSON(w, http.StatusCreated, map[string]any{
		"secret": sec, "enabled": b.Enabled, "note": "请立即绑定到 Authenticator，明文不再回显",
	})
}

func (d Deps) handleTOTPStatus(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": d.Approvals.TOTPEnabled(r.Context(), sess.UserID),
	})
}

func (d Deps) handleStepUp(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Password == "" {
		writeErr(w, http.StatusBadRequest, "需要 password")
		return
	}
	if d.Approvals != nil && d.Approvals.TOTPEnabled(r.Context(), sess.UserID) && body.TOTP == "" {
		writeErr(w, http.StatusForbidden, approval.ErrTOTPRequired.Error())
		return
	}
	out, err := d.Auth.StepUp(r.Context(), sess.Token, body.Password, clientIP(r), 10*time.Minute)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if d.Approvals != nil && d.Approvals.TOTPEnabled(r.Context(), sess.UserID) {
		if err := d.Approvals.VerifyUserTOTP(r.Context(), sess.UserID, body.TOTP, clientIP(r)); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"step_up_until": out.StepUpUntil, "ok": true,
	})
}
