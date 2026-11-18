package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/collector"
)

func (d Deps) collectorRoutes(mux *http.ServeMux) {
	authenticated := func(next authed) http.HandlerFunc {
		return d.requireExternalMCPSession(func(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
			if d.Collector == nil {
				writeErr(w, 503, "工作采集服务未启用")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
			next(w, r, sess)
		})
	}
	mux.HandleFunc("GET /api/collector/profile", authenticated(d.handleCollectorProfile))
	mux.HandleFunc("PUT /api/collector/profile", authenticated(d.handleCollectorProfile))
	mux.HandleFunc("POST /api/collector/oauth/start", authenticated(d.handleCollectorOAuthStart))
	mux.HandleFunc("GET /api/collector/oauth/callback", d.handleCollectorOAuthCallback)
	mux.HandleFunc("GET /api/collector/records/{kind}", authenticated(d.handleCollectorRecords))
	mux.HandleFunc("PUT /api/collector/targets/{id}", authenticated(d.handleCollectorSaveTarget))
	mux.HandleFunc("POST /api/collector/targets", authenticated(d.handleCollectorSaveTarget))
	mux.HandleFunc("PUT /api/collector/rules/{id}", authenticated(d.handleCollectorSaveRule))
	mux.HandleFunc("POST /api/collector/rules", authenticated(d.handleCollectorSaveRule))
	mux.HandleFunc("DELETE /api/collector/records/{kind}/{id}", authenticated(d.handleCollectorDelete))
	mux.HandleFunc("POST /api/collector/preview", authenticated(d.handleCollectorPreview))
	mux.HandleFunc("POST /api/collector/targets/{id}/scan", authenticated(d.handleCollectorScan))
	mux.HandleFunc("POST /api/collector/discover", authenticated(d.handleCollectorDiscover))
	mux.HandleFunc("POST /api/collector/rules/preview", authenticated(d.handleCollectorRulePreview))
	mux.HandleFunc("GET /api/collector/items", authenticated(d.handleCollectorItems))
	mux.HandleFunc("POST /api/collector/deliveries/{id}/retry", authenticated(d.handleCollectorRetry))
	mux.HandleFunc("POST /api/collector/import", authenticated(d.handleCollectorImport))
	mux.HandleFunc("POST /api/collector/webhooks/{owner}/{target}", d.handleCollectorWebhook)
}
func (d Deps) collectorOwner(w http.ResponseWriter, r *http.Request, sess *auth.Session, write bool) string {
	owner := r.URL.Query().Get("owner_id")
	if owner == "" || owner == sess.UserID {
		return sess.UserID
	}
	permission := "workflow.read"
	if write {
		permission = "workflow.write"
	}
	if !d.canManageExternalMCP(r, sess) || !d.scopeAll(r, sess, permission) {
		writeErr(w, 403, "无权管理其他用户的采集配置")
		return ""
	}
	u, err := d.Auth.Users().FindByID(r.Context(), owner)
	if err != nil || u == nil {
		writeErr(w, 404, "用户不存在")
		return ""
	}
	return owner
}
func collectorError(w http.ResponseWriter, err error) {
	status := 400
	if errors.Is(err, collector.ErrNotFound) {
		status = 404
	}
	if errors.Is(err, collector.ErrBusy) {
		status = 409
	}
	writeErr(w, status, err.Error())
}
func decodeCollector(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(v) != nil {
		writeErr(w, 400, "配置格式无效，请检查字段")
		return false
	}
	return true
}
func (d Deps) handleCollectorProfile(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	// Personal OAuth secrets can only be changed by the credential owner.
	if r.Method == "PUT" {
		var in collector.ProfileInput
		if !decodeCollector(w, r, &in) {
			return
		}
		if err := d.Collector.UpdateProfile(r.Context(), sess.UserID, in); err != nil {
			collectorError(w, err)
			return
		}
		d.auditUser(r, sess, "collector.profile.update", nil)
	}
	out, err := d.Collector.ProfileStatus(r.Context(), sess.UserID)
	if err != nil {
		collectorError(w, err)
		return
	}
	endpoint, _ := d.collectorCallbackURL(r)
	u, _ := url.Parse(endpoint)
	if u != nil {
		u.Path = "/api/collector/oauth/callback"
		u.RawQuery = ""
		u.Fragment = ""
		out["redirect_uri"] = u.String()
	}
	writeJSON(w, 200, out)
}
func (d Deps) collectorCallbackURL(r *http.Request) (string, error) {
	// OAuth redirects must share the browser's cookie origin. A separately
	// hosted public MCP endpoint is not necessarily the management site.
	if d.Config != nil && d.Config.PublicServerURL != "" {
		return strings.TrimRight(d.Config.PublicServerURL, "/") + "/api/collector/oauth/callback", nil
	}
	copyDeps := d
	copyDeps.Config = nil
	return copyDeps.externalMCPURL(r)
}
func (d Deps) handleCollectorOAuthStart(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	endpoint, err := d.collectorCallbackURL(r)
	if err != nil {
		collectorError(w, err)
		return
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		collectorError(w, err)
		return
	}
	u.Path = "/api/collector/oauth/callback"
	u.RawQuery = ""
	u.Fragment = ""
	nonce := make([]byte, 32)
	if _, err = rand.Read(nonce); err != nil {
		writeErr(w, 500, "生成授权会话失败")
		return
	}
	browser := base64.RawURLEncoding.EncodeToString(nonce)
	authorize, err := d.Collector.BeginOAuth(r.Context(), sess.UserID, u.String(), browser)
	if err != nil {
		collectorError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "aie_collector_oauth", Value: browser, Path: "/api/collector/oauth", HttpOnly: true, Secure: cookieSecure(r, d.Config), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	writeJSON(w, 200, map[string]string{"authorize_url": authorize, "redirect_uri": u.String()})
}
func (d Deps) handleCollectorOAuthCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if d.Collector == nil {
		writeErr(w, 503, "工作采集服务未启用")
		return
	}
	cookie, err := r.Cookie("aie_collector_oauth")
	if err != nil {
		writeErr(w, 400, "授权浏览器会话已过期，请回个人设置重新发起")
		return
	}
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/profile?feishu=cancelled", http.StatusSeeOther)
		return
	}
	code, state := r.URL.Query().Get("code"), r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeErr(w, 400, "缺少授权回调参数")
		return
	}
	if _, err = d.Collector.CompleteOAuth(r.Context(), state, code, cookie.Value); err != nil {
		collectorError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "aie_collector_oauth", Value: "", Path: "/api/collector/oauth", HttpOnly: true, Secure: cookieSecure(r, d.Config), SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/profile?feishu=authorized", http.StatusSeeOther)
}
func (d Deps) handleCollectorRecords(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, false)
	if owner == "" {
		return
	}
	rows, err := d.Collector.List(r.Context(), owner, r.PathValue("kind"))
	if err != nil {
		collectorError(w, err)
		return
	}
	// Bounded view; scanning and aggregation use the full persisted data.
	items := []json.RawMessage{}
	for _, row := range rows[:min(500, len(rows))] {
		items = append(items, row.Body)
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": len(rows)})
}
func (d Deps) handleCollectorSaveTarget(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, true)
	if owner == "" {
		return
	}
	var t collector.Target
	if !decodeCollector(w, r, &t) {
		return
	}
	if id := r.PathValue("id"); id != "" {
		t.ID = id
	}
	result, err := d.Collector.SaveTarget(r.Context(), owner, t)
	if err != nil {
		collectorError(w, err)
		return
	}
	d.auditUser(r, sess, "collector.target.save", map[string]string{"owner_id": owner, "target_id": result.ID})
	writeJSON(w, 200, result)
}
func (d Deps) handleCollectorSaveRule(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, true)
	if owner == "" {
		return
	}
	var rule collector.Rule
	if !decodeCollector(w, r, &rule) {
		return
	}
	if id := r.PathValue("id"); id != "" {
		rule.ID = id
	}
	result, err := d.Collector.SaveRule(r.Context(), owner, rule)
	if err != nil {
		collectorError(w, err)
		return
	}
	d.auditUser(r, sess, "collector.rule.save", map[string]string{"owner_id": owner, "rule_id": result.ID})
	writeJSON(w, 200, result)
}
func (d Deps) handleCollectorDelete(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, true)
	if owner == "" {
		return
	}
	if err := d.Collector.Delete(r.Context(), owner, r.PathValue("kind"), r.PathValue("id")); err != nil {
		collectorError(w, err)
		return
	}
	d.auditUser(r, sess, "collector.config.delete", map[string]string{"owner_id": owner, "id": r.PathValue("id")})
	w.WriteHeader(204)
}
func (d Deps) handleCollectorPreview(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, false)
	if owner == "" {
		return
	}
	var t collector.Target
	if !decodeCollector(w, r, &t) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	run, err := d.Collector.Scan(ctx, owner, t.ID, true, &t)
	if err != nil {
		collectorError(w, err)
		return
	}
	writeJSON(w, 200, run)
}
func (d Deps) handleCollectorScan(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, true)
	if owner == "" {
		return
	}
	run, err := d.Collector.RequestScan(r.Context(), owner, r.PathValue("id"))
	if err != nil {
		collectorError(w, err)
		return
	}
	d.auditUser(r, sess, "collector.scan", map[string]string{"owner_id": owner, "target_id": run.TargetID})
	writeJSON(w, http.StatusAccepted, run)
}
func (d Deps) handleCollectorDiscover(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, false)
	if owner == "" {
		return
	}
	var t collector.Target
	if !decodeCollector(w, r, &t) {
		return
	}
	t.OwnerID = owner
	result, err := d.Collector.Discover(r.Context(), t)
	if err != nil {
		collectorError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (d Deps) handleCollectorRulePreview(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, false)
	if owner == "" {
		return
	}
	var rule collector.Rule
	if !decodeCollector(w, r, &rule) {
		return
	}
	out, err := d.Collector.PreviewRule(r.Context(), owner, rule)
	if err != nil {
		collectorError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": out})
}
func (d Deps) handleCollectorItems(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, false)
	if owner == "" {
		return
	}
	items, err := d.Collector.Items(r.Context(), owner)
	if err != nil {
		collectorError(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}
func (d Deps) handleCollectorRetry(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	owner := d.collectorOwner(w, r, sess, true)
	if owner == "" {
		return
	}
	if err := d.Collector.RetryDelivery(r.Context(), owner, r.PathValue("id")); err != nil {
		collectorError(w, err)
		return
	}
	w.WriteHeader(204)
}
func (d Deps) handleCollectorImport(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
		Apply   bool   `json:"apply"`
	}
	if !decodeCollector(w, r, &in) {
		return
	}
	result, err := d.Collector.Import(r.Context(), sess.UserID, in.Kind, in.Content, in.Apply)
	if err != nil {
		collectorError(w, err)
		return
	}
	writeJSON(w, 200, result)
}
func (d Deps) handleCollectorWebhook(w http.ResponseWriter, r *http.Request) {
	if d.Collector == nil {
		writeErr(w, 503, "服务不可用")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	if err := d.Collector.Webhook(r.Context(), r.PathValue("owner"), r.PathValue("target"), r.Header, r.Body); err != nil {
		if strings.Contains(err.Error(), "签名") {
			writeErr(w, 401, "签名校验失败")
		} else {
			collectorError(w, err)
		}
		return
	}
	w.WriteHeader(202)
}
