package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/ai-employee-platform/server/internal/approval"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/permission"
	"github.com/ai-employee-platform/server/internal/secret"
)

func (d Deps) handlePutSecret(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var body struct {
		Name        string `json:"name"`
		Value       string `json:"value"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Name == "" || body.Value == "" {
		writeErr(w, http.StatusBadRequest, "需要 name/value")
		return
	}
	if d.SecretMgr != nil {
		meta, err := d.SecretMgr.Put(r.Context(), body.Name, body.Value, sess.UserID, clientIP(r), body.Description)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, meta)
		return
	}
	ref, err := d.Secrets.Put(body.Name, body.Value)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	d.Audit.Log(r.Context(), "USER", sess.UserID, "secret.put", "success", clientIP(r), map[string]string{
		"name": body.Name, "value": secret.Redact(body.Value), "secret_id": ref.ID,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"id": ref.ID, "name": ref.Name, "masked": "***"})
}

func (d Deps) handleListSecrets(w http.ResponseWriter, _ *http.Request, _ *auth.Session) {
	if d.SecretMgr != nil {
		writeJSON(w, http.StatusOK, map[string]any{"items": d.SecretMgr.ListMeta()})
		return
	}
	items := d.Secrets.List()
	out := make([]map[string]any, 0, len(items))
	for _, r := range items {
		out = append(out, map[string]any{"id": r.ID, "name": r.Name, "masked": "***"})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (d Deps) handleGetSecret(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.SecretMgr == nil {
		writeErr(w, http.StatusServiceUnavailable, "secret manager 未启用")
		return
	}
	meta, err := d.SecretMgr.GetMeta(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (d Deps) handleDeleteSecret(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.SecretMgr == nil {
		writeErr(w, http.StatusServiceUnavailable, "secret manager 未启用")
		return
	}
	if err := d.SecretMgr.Delete(r.Context(), r.PathValue("id"), sess.UserID, clientIP(r)); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (d Deps) handleBindSecret(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.SecretMgr == nil {
		writeErr(w, http.StatusServiceUnavailable, "secret manager 未启用")
		return
	}
	var body struct {
		EmployeeID string `json:"employee_id"`
		SecretID   string `json:"secret_id"`
		Purpose    string `json:"purpose"`
	}
	if err := decodeJSON(r, &body); err != nil || body.EmployeeID == "" || body.SecretID == "" {
		writeErr(w, http.StatusBadRequest, "需要 employee_id/secret_id")
		return
	}
	b, err := d.SecretMgr.Bind(r.Context(), body.EmployeeID, body.SecretID, body.Purpose, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, b)
}

func (d Deps) handleResolveSecrets(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.SecretMgr == nil {
		writeErr(w, http.StatusServiceUnavailable, "secret manager 未启用")
		return
	}
	empID := r.PathValue("employee_id")
	resolved, err := d.SecretMgr.ResolveForEmployee(r.Context(), empID, "USER", sess.UserID, clientIP(r))
	if err != nil {
		// 不回显 Secret
		writeErr(w, http.StatusConflict, "Secret 解析失败")
		return
	}
	// 仅返回 env key + secret_id，绝不回传明文
	safe := make([]map[string]string, 0, len(resolved))
	for _, x := range resolved {
		safe = append(safe, map[string]string{"env_key": x.EnvKey, "secret_id": x.SecretID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": safe, "count": len(safe)})
}

func (d Deps) handleRotateSecret(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.SecretMgr == nil || d.Approvals == nil {
		writeErr(w, http.StatusServiceUnavailable, "secret/approval 未启用")
		return
	}
	var body struct {
		Value string `json:"value"`
		TOTP  string `json:"totp"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Value == "" {
		writeErr(w, http.StatusBadRequest, "需要 value")
		return
	}
	sid := r.PathValue("id")
	// CRITICAL：credential.rotate — 无 TOTP 配置则 DENY
	dcs, ar, err := d.Approvals.EvaluateAndMaybeCreate(r.Context(), permission.Request{
		ActorType: "USER", ActorID: sess.UserID, Action: permission.ActionCredentialRot,
		TargetType: "secret", TargetID: sid,
	}, sess.UserID)
	if err == approval.ErrTOTPNotConfigured || dcs.Effect == permission.EffectDeny {
		writeErr(w, http.StatusForbidden, "CRITICAL 轮换被拒绝（需配置 TOTP）")
		return
	}
	if dcs.Effect == permission.EffectAsk && ar != nil && ar.Status == approval.StatusPending {
		// 同步路径：若已有 TOTP，直接 Approve
		if _, err := d.Approvals.Approve(r.Context(), ar.ID, sess.UserID, body.TOTP, clientIP(r)); err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
	}
	meta, err := d.SecretMgr.Rotate(r.Context(), sid, body.Value, sess.UserID, clientIP(r))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

func (d Deps) handleAuditExport(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 500
	}
	b, err := d.Audit.ExportJSON(audit.Filter{
		Actor:        r.URL.Query().Get("actor"),
		Action:       r.URL.Query().Get("action"),
		ActionPrefix: r.URL.Query().Get("prefix"),
		TargetType:   r.URL.Query().Get("target_type"),
		Limit:        limit,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=audit-export.json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}

func (d Deps) handleAuditArchive(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	// 仅 SUPER_ADMIN
	isSuper := false
	for _, role := range sess.Roles {
		if role == "SUPER_ADMIN" {
			isSuper = true
			break
		}
	}
	if !isSuper {
		writeErr(w, http.StatusForbidden, "仅超级管理员可归档审计")
		return
	}
	var body struct {
		OlderThanDays int `json:"older_than_days"`
	}
	_ = decodeJSON(r, &body)
	if body.OlderThanDays <= 0 {
		body.OlderThanDays = 90
	}
	n := d.Audit.ArchiveBefore(time.Now().UTC().AddDate(0, 0, -body.OlderThanDays))
	d.Audit.Log(r.Context(), "USER", sess.UserID, "audit.archive", "success", clientIP(r), map[string]string{
		"removed": strconv.Itoa(n),
	})
	writeJSON(w, http.StatusOK, map[string]any{"removed": n})
}

// 覆盖旧 handleAudit：支持 prefix / target
func (d Deps) handleAuditEnhanced(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	items := d.Audit.Query(audit.Filter{
		Actor:        r.URL.Query().Get("actor"),
		Action:       r.URL.Query().Get("action"),
		ActionPrefix: r.URL.Query().Get("prefix"),
		TargetType:   r.URL.Query().Get("target_type"),
		TargetID:     r.URL.Query().Get("target_id"),
		Limit:        limit,
	})
	// 确保 metadata 无明文 secret 值
	for i := range items {
		if items[i].Metadata != nil {
			if v, ok := items[i].Metadata["value"]; ok && v != "" && v != "***" {
				items[i].Metadata["value"] = secret.Redact(v)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "store": "audit"})
}