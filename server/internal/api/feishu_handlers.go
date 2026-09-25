package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/secret"
)

func (d Deps) handleFeishuGetConfig(w http.ResponseWriter, _ *http.Request, _ *auth.Session) {
	if d.Feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	writeJSON(w, http.StatusOK, d.Feishu.GetConfigPublic())
}

func (d Deps) handleFeishuStatus(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	forceRefresh := r.URL.Query().Get("refresh") == "true" || r.URL.Query().Get("refresh") == "1"
	status := d.Feishu.Status(r.Context(), forceRefresh)
	writeJSON(w, http.StatusOK, status)
}

func (d Deps) handleFeishuTestMessage(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	var req struct {
		ReceiveIDType string `json:"receive_id_type"`
		ReceiveID     string `json:"receive_id"`
		Content       string `json:"content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	if req.ReceiveID == "" {
		writeErr(w, http.StatusBadRequest, "需要 receive_id (用户 OpenID 或群聊 Chat ID)")
		return
	}

	res, err := d.Feishu.TestSendMessage(r.Context(), req.ReceiveIDType, req.ReceiveID, req.Content)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "feishu.test_message", "success", clientIP(r), map[string]string{
			"receive_id": req.ReceiveID,
			"message_id": res.MessageID,
		})
	}

	writeJSON(w, http.StatusOK, res)
}

func (d Deps) handleFeishuPutConfig(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Feishu == nil || d.Secrets == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	var body struct {
		AppID             string `json:"app_id"`
		AppSecret         string `json:"app_secret"` // 明文仅此一次，随后存引用
		VerificationToken string `json:"verification_token"`
		EncryptKey        string `json:"encrypt_key"`
		Enabled           bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, "无效请求体")
		return
	}
	cfg := feishu.Config{
		AppID:             body.AppID,
		VerificationToken: body.VerificationToken,
		Enabled:           body.Enabled,
	}
	if body.AppSecret != "" {
		ref, err := d.Secrets.Put("feishu.app_secret", body.AppSecret)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg.AppSecretRef = ref.ID
		_ = secret.Redact(body.AppSecret) // 提醒：不得记日志
	}
	if body.EncryptKey != "" {
		ref, err := d.Secrets.Put("feishu.encrypt_key", body.EncryptKey)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		cfg.EncryptKeyRef = ref.ID
	}
	// 保留已有引用
	old := d.Feishu.GetConfigPublic()
	if cfg.AppSecretRef == "" {
		cfg.AppSecretRef = old.AppSecretRef
	}
	if cfg.EncryptKeyRef == "" {
		cfg.EncryptKeyRef = old.EncryptKeyRef
	}
	d.Feishu.SetConfig(cfg)
	if d.Audit != nil {
		d.Audit.Log(r.Context(), "USER", sess.UserID, "feishu.config", "success", clientIP(r), map[string]string{
			"app_id": body.AppID, "app_secret": secret.Redact(body.AppSecret),
		})
	}
	writeJSON(w, http.StatusOK, d.Feishu.GetConfigPublic())
}

func (d Deps) handleFeishuBinding(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	var b feishu.Binding
	if err := decodeJSON(r, &b); err != nil || b.EmployeeID == "" {
		writeErr(w, http.StatusBadRequest, "需要 employee_id")
		return
	}
	if b.FeishuAlias == "" {
		writeErr(w, http.StatusBadRequest, "需要 feishu_bot_alias")
		return
	}
	d.Feishu.UpsertBinding(b)
	d.auditUser(r, sess, "feishu.binding.upsert", map[string]string{
		"employee_id": b.EmployeeID, "alias": b.FeishuAlias,
		"summary": fmt.Sprintf("员工 %s 绑定别名 @%s", b.EmployeeID, b.FeishuAlias),
	})
	writeJSON(w, http.StatusOK, b)
}

func (d Deps) handleDeleteFeishuBinding(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if d.Feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	empID := r.URL.Query().Get("employee_id")
	alias := r.URL.Query().Get("feishu_bot_alias")
	if alias == "" {
		alias = r.URL.Query().Get("alias")
	}
	if empID == "" && alias == "" {
		writeErr(w, http.StatusBadRequest, "需要 employee_id 或 feishu_bot_alias")
		return
	}
	if !d.Feishu.DeleteBinding(empID, alias) {
		writeErr(w, http.StatusNotFound, "绑定不存在")
		return
	}
	d.auditUser(r, sess, "feishu.binding.delete", map[string]string{
		"employee_id": empID, "alias": alias,
		"summary": fmt.Sprintf("删除员工 %s 的别名绑定 @%s", empID, alias),
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (d Deps) handleFeishuEvents(w http.ResponseWriter, r *http.Request) {
	if d.Feishu == nil {
		writeErr(w, http.StatusServiceUnavailable, "feishu 未启用")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "读取失败")
		return
	}
	ts := r.Header.Get("X-Lark-Request-Timestamp")
	nonce := r.Header.Get("X-Lark-Request-Nonce")
	sig := r.Header.Get("X-Lark-Signature")
	if sig == "" {
		sig = r.Header.Get("X-Lark-Signature-256")
	}
	// 有签名头才验签；URL challenge 可能无签名
	if sig != "" || (ts != "" && nonce != "") {
		if err := d.Feishu.VerifySignature(ts, nonce, sig, string(body)); err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
	}
	challenge, token, ev, err := feishu.ParseWebhookBody(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if challenge != "" {
		ch, err := d.Feishu.HandleURLChallenge(token, challenge)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"challenge": ch})
		return
	}
	if ev == nil {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	// HTTP 内不候 Agent：只建 Job 后 200
	jobID, dup, err := d.Feishu.HandleMessage(r.Context(), *ev)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "accepted", "job_id": jobID, "duplicate": dup,
	})
}

func (d Deps) handleSchedulerTick(w http.ResponseWriter, r *http.Request, _ *auth.Session) {
	if d.Scheduler == nil {
		writeErr(w, http.StatusServiceUnavailable, "scheduler 未启用")
		return
	}
	n := d.Scheduler.Tick(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"scheduled": n})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}
