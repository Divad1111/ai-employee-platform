package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcp"
	"github.com/ai-employee-platform/server/internal/mcpauth"
)

func (d Deps) handleExportMCPServers(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	w.Header().Set("Cache-Control", "no-store")
	if !d.scopeAll(r, sess, "workflow.read") {
		writeErr(w, http.StatusForbidden, "导出全部 MCP 需要全局读取权限")
		return
	}
	if d.MCP == nil || d.MCPAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP 服务未就绪")
		return
	}
	var in struct {
		Format         string `json:"format"`
		Scope          string `json:"scope"`
		CollectorScope string `json:"collector_scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "无效 JSON 数据")
		return
	}
	if in.Format == "" {
		in.Format = "json"
	}
	if in.Scope == "" {
		in.Scope = mcpauth.ScopeRead
	}
	if (in.Format != "json" && in.Format != "codex") || (in.Scope != mcpauth.ScopeRead && in.Scope != mcpauth.ScopeWrite) {
		writeErr(w, http.StatusBadRequest, "不支持的导出格式或授权范围")
		return
	}
	if !d.allowExternalMCPScope(w, r, sess, in.Scope) {
		return
	}
	builtinURL, err := d.externalMCPURL(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	config, _, warnings, err := d.MCP.PrepareExport(r.Context(), sess.UserID, builtinURL, in.Format)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	var token *mcpauth.Token
	tokens := []*mcpauth.Token{}
	revoke := func() {
		for _, t := range tokens {
			_ = d.MCPAuth.Revoke(r.Context(), t.ID)
		}
	}
	for _, entry := range config.MCPServers {
		if entry.BuiltinService == "" {
			continue
		}
		scope := in.Scope
		if entry.BuiltinService == mcpauth.Collector {
			scope = in.CollectorScope
			if scope == "" {
				scope = mcpauth.ScopeRead
			}
		}
		issued, err := d.MCPAuth.IssueFor(r.Context(), entry.BuiltinService, mcpauth.SubjectExternalUser, sess.UserID,
			scope, "MCP 导出 ("+in.Format+")", sess.UserID, 90*24*time.Hour)
		if err != nil {
			revoke()
			writeErr(w, http.StatusInternalServerError, "签发外部 MCP 授权失败")
			return
		}
		tokens = append(tokens, issued.Token)
		if token == nil || entry.BuiltinService == mcpauth.Workflow {
			token = issued.Token
		}
		entry.Headers = map[string]string{"Authorization": "Bearer " + issued.Secret}
	}
	content, filename, err := mcp.RenderExport(config, in.Format)
	if err != nil {
		revoke()
		writeErr(w, http.StatusInternalServerError, "生成 MCP 配置失败")
		return
	}
	tokenID := ""
	if token != nil {
		tokenID = token.ID
	}
	d.auditUser(r, sess, "mcp.export", map[string]string{
		"token_id": tokenID, "format": in.Format, "scope": in.Scope,
		"server_count": fmt.Sprint(len(config.MCPServers)),
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"filename": filename, "content": content, "server_count": len(config.MCPServers),
		"token": token, "tokens": tokens, "warnings": warnings,
	})
}

func (d Deps) handleListMCPExportTokens(w http.ResponseWriter, r *http.Request, sess *auth.Session) {
	w.Header().Set("Cache-Control", "no-store")
	if d.MCPAuth == nil {
		writeErr(w, http.StatusServiceUnavailable, "MCP 授权服务未就绪")
		return
	}
	items, err := d.MCPAuth.ListBySubject(r.Context(), mcpauth.SubjectExternalUser, sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取外部授权失败")
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (d Deps) externalMCPURL(r *http.Request) (string, error) {
	if d.Config != nil {
		if d.Config.MCPPublicURL != "" {
			return d.Config.MCPPublicURL, nil
		}
		if d.Config.PublicServerURL != "" {
			return strings.TrimRight(d.Config.PublicServerURL, "/") + "/mcp", nil
		}
	}
	// Browser POSTs include Origin. Keep its public port, which reverse proxies
	// may omit from Host. Cookie-authenticated requests already validate Origin.
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") ||
			u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return "", fmt.Errorf("无法确定外部 MCP 地址，请配置 AIE_MCP_PUBLIC_URL")
		}
		return strings.TrimRight(origin, "/") + "/mcp", nil
	}
	scheme := "http"
	// SecureHTTP strips forwarded headers from untrusted peers.
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/mcp", nil
}
