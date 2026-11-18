package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
)

// Self-service credentials need an authenticated account and the same MFA
// gate as management routes, but do not require a global workflow grant.
func (d Deps) requireExternalMCPSession(next authed) http.HandlerFunc {
	return d.requireAuth(func(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
		w.Header().Set("Cache-Control", "no-store")
		if sess.MFARequired && (!sess.MFAAuthenticated || d.Approvals == nil || !d.Approvals.TOTPEnabled(r.Context(), sess.UserID)) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "请先完成双因子验证", "needs_mfa_enrollment": true})
			return
		}
		if d.MCPAuth == nil {
			writeErr(w, http.StatusServiceUnavailable, "MCP 授权服务未就绪")
			return
		}
		next(w, r, sess)
	})
}

func (d Deps) canManageExternalMCP(r *http.Request, sess *auth.Session) bool {
	return (isSuperAdmin(sess) || containsRole(sess.Roles, "ADMIN")) && d.scopeAll(r, sess, "user.update")
}

func (d Deps) handleGetExternalMCPPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	userID := r.PathValue("id")
	if userID == "" {
		userID = sess.UserID
	}
	if userID != sess.UserID && !d.canManageExternalMCP(r, sess) {
		writeErr(w, http.StatusForbidden, "无权查看其他用户的外部 MCP 权限")
		return
	}
	if _, err := d.Auth.Users().FindByID(r.Context(), userID); err != nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	policy, err := d.MCPAuth.ExternalPolicy(r.Context(), userID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取外部 MCP 权限失败")
		return
	}
	writeJSON(w, http.StatusOK, policy)
}

func (d Deps) handleSetExternalMCPPolicy(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	if !d.canManageExternalMCP(r, sess) {
		writeErr(w, http.StatusForbidden, "仅具有全局用户管理权限的管理员可授予外部 MCP 写权限")
		return
	}
	userID := r.PathValue("id")
	u, err := d.Auth.Users().FindByID(r.Context(), userID)
	if err != nil || u == nil {
		writeErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if containsRole(u.Roles, "SUPER_ADMIN") && !isSuperAdmin(sess) {
		writeErr(w, http.StatusForbidden, "仅超级管理员可修改该用户的外部 MCP 权限")
		return
	}
	var in struct {
		CanWrite *bool  `json:"can_write"`
		Service  string `json:"service"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil || in.CanWrite == nil {
		writeErr(w, http.StatusBadRequest, "请指定 can_write")
		return
	}
	if in.Service == "" {
		in.Service = mcpauth.Workflow
	}
	policy, err := d.MCPAuth.SetExternalServicePolicy(r.Context(), userID, in.Service, *in.CanWrite, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存外部 MCP 权限失败")
		return
	}
	d.auditUser(r, sess, "mcp.external.policy", map[string]string{"user_id": userID, "can_write": strconv.FormatBool(policy.CanWrite)})
	writeJSON(w, http.StatusOK, policy)
}

func (d Deps) allowExternalMCPScope(w http.ResponseWriter, r *http.Request, sess *auth.Session, scope string) bool {
	if scope != mcpauth.ScopeWrite {
		return true
	}
	policy, err := d.MCPAuth.ExternalPolicy(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取外部 MCP 权限失败")
		return false
	}
	if !policy.CanWrite {
		writeErr(w, http.StatusForbidden, "尚未获管理员授予外部 MCP 写权限")
		return false
	}
	return true
}

func (d Deps) handleCreateExternalMCPToken(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	var in struct {
		Format   string `json:"format"`
		Scope    string `json:"scope"`
		Label    string `json:"label"`
		Services []struct {
			Service string `json:"service"`
			Scope   string `json:"scope"`
		} `json:"services"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if dec.Decode(&in) != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	if in.Format == "" {
		in.Format = "json"
	}
	if in.Scope == "" {
		in.Scope = mcpauth.ScopeRead
	}
	in.Label = strings.TrimSpace(in.Label)
	if in.Label == "" {
		in.Label = "个人外部 MCP (" + in.Format + ")"
	}
	if (in.Format != "json" && in.Format != "codex") ||
		(in.Scope != mcpauth.ScopeRead && in.Scope != mcpauth.ScopeWrite) || len([]rune(in.Label)) > 80 {
		writeErr(w, http.StatusBadRequest, "不支持的格式、授权范围或名称过长")
		return
	}
	if len(in.Services) == 0 && !d.allowExternalMCPScope(w, r, sess, in.Scope) {
		return
	}
	endpoint, err := d.externalMCPURL(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(in.Services) == 0 {
		in.Services = append(in.Services, struct {
			Service string `json:"service"`
			Scope   string `json:"scope"`
		}{mcpauth.Workflow, in.Scope})
	}
	if len(in.Services) > 2 {
		writeErr(w, 400, "最多选择两个内置服务")
		return
	}
	cfg := &mcp.ClientConfig{MCPServers: map[string]*mcp.ClientServer{}}
	tokens := []*mcpauth.Token{}
	revoke := func() {
		for _, t := range tokens {
			_ = d.MCPAuth.Revoke(r.Context(), t.ID)
		}
	}
	for _, selected := range in.Services {
		builtin, ok := mcpauth.FindBuiltin(selected.Service)
		if !ok || builtin.Name != selected.Service || cfg.MCPServers[builtin.Name] != nil {
			revoke()
			writeErr(w, 400, "无效或重复的内置服务")
			return
		}
		if selected.Scope == "" {
			selected.Scope = mcpauth.ScopeRead
		}
		if d.MCP != nil {
			srv, e := d.MCP.GetServer(r.Context(), builtin.ID)
			if e != nil || srv.Status != mcp.StatusActive {
				revoke()
				writeErr(w, 400, "内置服务未启用")
				return
			}
		}
		issued, e := d.MCPAuth.IssueFor(r.Context(), builtin.Name, mcpauth.SubjectExternalUser, sess.UserID,
			selected.Scope, in.Label, sess.UserID, 90*24*time.Hour)
		if e != nil {
			revoke()
			writeErr(w, 403, "服务权限不足或授权范围无效")
			return
		}
		tokens = append(tokens, issued.Token)
		cfg.MCPServers[builtin.Name] = &mcp.ClientServer{Type: "http", URL: mcpauth.Endpoint(endpoint, builtin.Name), Headers: map[string]string{"Authorization": "Bearer " + issued.Secret}}
	}
	content, filename, err := mcp.RenderExport(cfg, in.Format)
	if err != nil {
		revoke()
		writeErr(w, http.StatusInternalServerError, "生成 MCP 配置失败")
		return
	}
	d.auditUser(r, sess, "mcp.external.create", map[string]string{"token_id": tokens[0].ID, "scope": in.Scope})
	writeJSON(w, http.StatusCreated, map[string]any{
		"filename": filename, "content": content, "server_count": len(tokens), "token": tokens[0], "tokens": tokens,
	})
}

func (d Deps) handleRevokeOwnExternalMCPToken(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	t, err := d.MCPAuth.Get(r.Context(), r.PathValue("id"))
	if err != nil || t.SubjectType != mcpauth.SubjectExternalUser || t.SubjectID != sess.UserID || t.CreatedBy != sess.UserID {
		writeErr(w, http.StatusNotFound, "外部凭证不存在")
		return
	}
	if err := d.MCPAuth.Revoke(r.Context(), t.ID); err != nil {
		writeErr(w, http.StatusInternalServerError, "吊销外部凭证失败")
		return
	}
	d.auditUser(r, sess, "mcp.external.revoke", map[string]string{"token_id": t.ID})
	w.WriteHeader(http.StatusNoContent)
}
