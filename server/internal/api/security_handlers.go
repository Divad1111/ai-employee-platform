package api

import (
	"errors"
	"net/http"
	"strings"
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
	if d.Approvals == nil {
		writeErr(w, http.StatusServiceUnavailable, "approvals 未启用")
		return
	}
	var body struct {
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		Code     string `json:"code"`
	}
	_ = decodeJSON(r, &body)

	ip := clientIP(r)
	alreadyEnabled := d.Approvals.TOTPEnabled(r.Context(), sess.UserID)
	password := strings.TrimSpace(body.Password)
	totpCode := strings.TrimSpace(body.Code)
	if totpCode == "" {
		totpCode = strings.TrimSpace(body.TOTP)
	}

	// 若已启用 TOTP，重新生成绑定二维码属于高危凭证重置，必须同时输入管理员登录密码与当前 6 位动态口令
	if alreadyEnabled {
		if password == "" && d.Auth != nil {
			if d.Audit != nil {
				d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.re_enroll", "failed", ip, map[string]string{"reason": "missing_password"})
			}
			writeErr(w, http.StatusForbidden, "重新生成绑定二维码需输入管理员登录密码")
			return
		}
		if d.Auth != nil && password != "" {
			if err := d.Auth.VerifyPassword(r.Context(), sess.Username, password); err != nil {
				if d.Audit != nil {
					d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.re_enroll", "failed", ip, map[string]string{"reason": "invalid_password"})
				}
				writeErr(w, http.StatusForbidden, "管理员登录密码错误")
				return
			}
		}

		if totpCode == "" {
			if d.Audit != nil {
				d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.re_enroll", "failed", ip, map[string]string{"reason": "missing_totp"})
			}
			writeErr(w, http.StatusForbidden, "重新生成绑定二维码需输入当前 Authenticator 中的 6 位动态口令")
			return
		}
		if err := d.Approvals.VerifyUserTOTP(r.Context(), sess.UserID, totpCode, ip); err != nil {
			if d.Audit != nil {
				d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.re_enroll", "failed", ip, map[string]string{"reason": "invalid_totp"})
			}
			writeErr(w, http.StatusForbidden, "当前 TOTP 动态口令校验失败，拒绝重新生成")
			return
		}
	} else {
		// 未开启时首次开通：需验证管理员登录密码
		if password == "" && d.Auth != nil {
			if d.Audit != nil {
				d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.enroll", "failed", ip, map[string]string{"reason": "missing_password"})
			}
			writeErr(w, http.StatusForbidden, "开通 TOTP 双因子认证需输入管理员登录密码")
			return
		}
		if d.Auth != nil && password != "" {
			if err := d.Auth.VerifyPassword(r.Context(), sess.Username, password); err != nil {
				if d.Audit != nil {
					d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.enroll", "failed", ip, map[string]string{"reason": "invalid_password"})
				}
				writeErr(w, http.StatusForbidden, "管理员登录密码错误")
				return
			}
		}
	}

	sec, b, err := d.Approvals.EnrollPendingTOTP(r.Context(), sess.UserID, ip)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if alreadyEnabled && d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.re_enroll", "success", ip, map[string]string{"ref": b.SecretRef})
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"secret":  sec,
		"enabled": false,
		"note":    "请使用手机 Authenticator 扫描二维码并输入 6 位动态口令进行首次激活确认",
	})
}

func (d Deps) handleConfirmTOTP(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Approvals == nil {
		writeErr(w, http.StatusServiceUnavailable, "approvals 未启用")
		return
	}
	var body struct {
		TOTP string `json:"totp"`
		Code string `json:"code"`
	}
	_ = decodeJSON(r, &body)

	code := strings.TrimSpace(body.Code)
	if code == "" {
		code = strings.TrimSpace(body.TOTP)
	}
	if code == "" {
		writeErr(w, http.StatusBadRequest, "请输入手机 Authenticator 生成的 6 位动态口令以确认激活")
		return
	}

	ip := clientIP(r)
	if err := d.Approvals.ConfirmTOTP(r.Context(), sess.UserID, code, ip); err != nil {
		if d.Audit != nil {
			d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.confirm", "failed", ip, map[string]string{"error": err.Error()})
		}
		writeErr(w, http.StatusForbidden, "动态口令校验失败，请核对手机时间或重新输入")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": true,
		"message": "TOTP 双因子认证已成功激活生效",
	})
}

func (d Deps) handleDisableTOTP(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Approvals == nil {
		writeErr(w, http.StatusServiceUnavailable, "approvals 未启用")
		return
	}
	var body struct {
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		Code     string `json:"code"`
	}
	_ = decodeJSON(r, &body)

	ip := clientIP(r)
	if !d.Approvals.TOTPEnabled(r.Context(), sess.UserID) {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "message": "当前未开启 TOTP"})
		return
	}

	password := strings.TrimSpace(body.Password)
	totpCode := strings.TrimSpace(body.Code)
	if totpCode == "" {
		totpCode = strings.TrimSpace(body.TOTP)
	}

	// 1. 验证管理员登录密码
	if password == "" && d.Auth != nil {
		if d.Audit != nil {
			d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.disable", "failed", ip, map[string]string{"reason": "missing_password"})
		}
		writeErr(w, http.StatusForbidden, "关闭 TOTP 双因子认证需输入管理员登录密码")
		return
	}
	if d.Auth != nil && password != "" {
		if err := d.Auth.VerifyPassword(r.Context(), sess.Username, password); err != nil {
			if d.Audit != nil {
				d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.disable", "failed", ip, map[string]string{"reason": "invalid_password"})
			}
			writeErr(w, http.StatusForbidden, "管理员登录密码错误")
			return
		}
	}

	// 2. 验证当前 6 位 TOTP 口令
	if totpCode == "" {
		if d.Audit != nil {
			d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.disable", "failed", ip, map[string]string{"reason": "missing_totp"})
		}
		writeErr(w, http.StatusForbidden, "关闭 TOTP 双因子认证需输入当前 Authenticator 中的 6 位动态口令")
		return
	}
	if err := d.Approvals.VerifyUserTOTP(r.Context(), sess.UserID, totpCode, ip); err != nil {
		if d.Audit != nil {
			d.Audit.Log(r.Context(), "USER", sess.UserID, "totp.disable", "failed", ip, map[string]string{"reason": "invalid_totp"})
		}
		writeErr(w, http.StatusForbidden, "当前 TOTP 动态口令校验失败，拒绝关闭")
		return
	}

	// 3. 执行关闭
	if err := d.Approvals.DisableTOTP(r.Context(), sess.UserID, ip); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": false,
		"message": "TOTP 双因子认证已成功安全关闭",
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
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Password) == "" {
		writeErr(w, http.StatusBadRequest, "需要 password")
		return
	}
	// 1. 先校验密码（不提前延长二次认证窗口）
	if d.Auth != nil {
		if err := d.Auth.VerifyPassword(r.Context(), sess.Username, body.Password); err != nil {
			// 密码错误只表示二次认证失败，登录会话仍然有效。
			writeErr(w, http.StatusForbidden, "密码错误")
			return
		}
	}
	// 2. 若启用了 TOTP，必须通过 2FA 动态口令校验
	if d.Approvals != nil && d.Approvals.TOTPEnabled(r.Context(), sess.UserID) {
		if strings.TrimSpace(body.TOTP) == "" {
			writeErr(w, http.StatusForbidden, approval.ErrTOTPRequired.Error())
			return
		}
		if err := d.Approvals.VerifyUserTOTP(r.Context(), sess.UserID, body.TOTP, clientIP(r)); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
	}
	// 3. 密码及 TOTP 全部校验通过后，才延长 Step-Up 状态
	out, err := d.Auth.StepUp(r.Context(), sess.Token, body.Password, clientIP(r), 10*time.Minute)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeErr(w, http.StatusForbidden, "密码错误")
			return
		}
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"step_up_until": out.StepUpUntil, "ok": true,
	})
}
