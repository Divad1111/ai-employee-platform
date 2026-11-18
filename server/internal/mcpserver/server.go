// Package mcpserver 实现工作流MCP 的 JSON-RPC 2.0 / Streamable HTTP 端点。
package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"github.com/ai-employee-platform/server/internal/workflowmcp"
)

// Caller 当前调用方身份。
type Caller struct {
	Kind          string // employee | user | admin_session
	SubjectID     string
	Scope         string // READ | ADMIN
	CanWrite      bool
	CanDelete     bool
	EmployeeScope *workflowmcp.Scope // employee 时非空
	UserID        string
	TokenID       string
	AllowedTools  []string
	DeniedTools   []string
}

type ctxKey int

const callerKey ctxKey = 1

// WithCaller 注入 Caller。
func WithCaller(ctx context.Context, c *Caller) context.Context {
	return context.WithValue(ctx, callerKey, c)
}

// CallerFrom 取出 Caller。
func CallerFrom(ctx context.Context) *Caller {
	c, _ := ctx.Value(callerKey).(*Caller)
	return c
}

// Server MCP 服务。
type Server struct {
	WF       *workflowmcp.Service
	MCPAuth  *mcpauth.Service
	Auth     *auth.Service
	Syncer   SkillSyncer // 可选：同步技能到工作站
	Provider Provider
	// Called on every request, so binding removal and owner changes take effect.
	ResolveEmployee func(context.Context, string, string) (string, []string, []string, error)
	Enabled         func(context.Context, string) bool
}

// Provider shares protocol and authorization across built-in MCP services.
type Provider interface {
	Name() string
	Tools() []map[string]any
	Call(context.Context, *Caller, string, map[string]any) (any, error)
}

func (s *Server) name() string {
	if s.Provider != nil {
		return s.Provider.Name()
	}
	return mcpauth.Workflow
}

// SkillSyncer 技能同步接口。
type SkillSyncer interface {
	SyncSkills(ctx context.Context, workstationID, employeeID string, skillIDs []string) error
}

// JSON-RPC 结构。
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id,omitempty"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Handler 返回 HTTP Handler（POST /mcp）。
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, http.StatusOK, map[string]any{
				"name": s.name(), "version": "1.0.0",
				"transport": "streamable-http",
			})
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		caller, err := s.authenticate(r)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
			return
		}
		ctx := WithCaller(r.Context(), caller)

		r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			return
		}
		if req.JSONRPC == "" {
			req.JSONRPC = "2.0"
		}
		// Streamable HTTP notifications (including notifications/initialized)
		// are acknowledged with an empty 202 response. A JSON-RPC result without
		// an id makes strict MCP clients fail the handshake before tools/list.
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		result, rpcErr := s.dispatch(ctx, req.Method, req.Params)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		writeRPC(w, resp)
	})
}

func (s *Server) authenticate(r *http.Request) (*Caller, error) {
	if s.Enabled != nil && !s.Enabled(r.Context(), s.name()) {
		return nil, auth.ErrForbidden
	}
	raw := bearer(r)
	if raw == "" {
		return nil, fmt.Errorf("缺少 Authorization")
	}
	// MCP Token
	if strings.HasPrefix(raw, mcpauth.Prefix) {
		if s.MCPAuth == nil {
			return nil, fmt.Errorf("MCP Auth 未启用")
		}
		t, err := s.MCPAuth.Validate(r.Context(), raw)
		if err != nil {
			return nil, err
		}
		if !t.AllowsService(s.name()) {
			return nil, auth.ErrForbidden
		}
		c := &Caller{SubjectID: t.SubjectID, Scope: t.Scope, TokenID: t.ID}
		if t.SubjectType == mcpauth.SubjectEmployee {
			c.Kind = "employee"
			c.CanWrite = false
			if s.ResolveEmployee != nil {
				owner, allowed, denied, err := s.ResolveEmployee(r.Context(), t.SubjectID, s.name())
				if err != nil {
					return nil, err
				}
				c.UserID, c.AllowedTools, c.DeniedTools = owner, allowed, denied
			}
			if s.Provider != nil {
				if c.UserID == "" {
					return nil, auth.ErrForbidden
				}
				return c, nil
			}
			if s.WF == nil {
				return nil, fmt.Errorf("员工授权范围不可用")
			}
			if s.WF != nil {
				scope, err := s.WF.ResolveScope(r.Context(), t.SubjectID)
				if err != nil || scope == nil {
					return nil, fmt.Errorf("员工授权范围不可用")
				}
				c.EmployeeScope = scope
			}
		} else if t.SubjectType == mcpauth.SubjectUser || t.SubjectType == mcpauth.SubjectExternalUser {
			c.Kind = "user"
			c.UserID = t.SubjectID
			if s.Auth == nil {
				return nil, fmt.Errorf("用户授权不可用")
			}
			u, err := s.Auth.Users().FindByID(r.Context(), t.SubjectID)
			if err != nil || u == nil || u.Status != auth.StatusActive {
				return nil, auth.ErrUnauthorized
			}
			session := &auth.Session{UserID: u.ID, Username: u.Username, Roles: u.Roles}
			if t.SubjectType == mcpauth.SubjectExternalUser {
				// Self-service delegation is separate from platform RBAC.
				// Legacy external ADMIN tokens are rejected; reissue READ/WRITE.
				if t.CreatedBy != t.SubjectID || t.ExpiresAt == nil ||
					(t.Scope != mcpauth.ScopeRead && t.Scope != mcpauth.ScopeWrite) {
					return nil, auth.ErrForbidden
				}
				policy, err := s.MCPAuth.ExternalPolicy(r.Context(), u.ID)
				if err != nil {
					return nil, auth.ErrForbidden
				}
				c.Kind = "external_user"
				c.CanWrite = t.Scope == mcpauth.ScopeWrite && policy.CanWriteService(s.name())
				// Write permission never includes deletion.
				return c, nil
			}
			if s.Auth.Authorize(r.Context(), session, "workflow.read") != nil {
				return nil, auth.ErrForbidden
			}
			if s.Auth.RequiresMFA(r.Context(), u.Roles) {
				return nil, fmt.Errorf("管理员请使用已完成双因子验证的 Web 会话")
			}
			c.CanWrite = t.Scope == mcpauth.ScopeAdmin && s.Auth.AuthorizeAll(r.Context(), session, "workflow.write") == nil
			c.CanDelete = t.Scope == mcpauth.ScopeAdmin && s.Auth.AuthorizeAll(r.Context(), session, "workflow.delete") == nil
		} else {
			return nil, auth.ErrUnauthorized
		}
		return c, nil
	}
	// Admin 会话 Token
	if s.Auth == nil {
		return nil, fmt.Errorf("Auth 未启用")
	}
	sess, err := s.Auth.Authenticate(r.Context(), raw)
	if err != nil {
		return nil, err
	}
	if sess.MFARequired && !sess.MFAAuthenticated {
		return nil, auth.ErrForbidden
	}
	if s.Auth.Authorize(r.Context(), sess, "workflow.read") != nil {
		return nil, auth.ErrForbidden
	}
	canWrite := s.Auth.AuthorizeAll(r.Context(), sess, "workflow.write") == nil
	return &Caller{
		Kind: "admin_session", SubjectID: sess.UserID, UserID: sess.UserID,
		Scope: mcpauth.ScopeAdmin, CanWrite: canWrite, CanDelete: s.Auth.AuthorizeAll(r.Context(), sess, "workflow.delete") == nil,
	}, nil
}

func (s *Server) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name(), "version": "1.0.0"},
		}, nil
	case "notifications/initialized", "ping":
		return map[string]any{}, nil
	case "tools/list":
		definitions := toolDefs()
		if s.Provider != nil {
			definitions = s.Provider.Tools()
		}
		tools := []map[string]any{}
		for _, tool := range definitions {
			if toolAllowed(CallerFrom(ctx), fmt.Sprint(tool["name"])) {
				tools = append(tools, tool)
			}
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params"}
		}
		var out any
		var err error
		if !toolAllowed(CallerFrom(ctx), p.Name) {
			err = auth.ErrForbidden
		} else if s.Provider != nil {
			out, err = s.Provider.Call(ctx, CallerFrom(ctx), p.Name, p.Arguments)
		} else {
			out, err = s.callTool(ctx, p.Name, p.Arguments)
		}
		if err != nil {
			return map[string]any{
				"content": []map[string]any{{"type": "text", "text": err.Error()}},
				"isError": true,
			}, nil
		}
		text, _ := json.MarshalIndent(out, "", "  ")
		return map[string]any{
			"content": []map[string]any{{"type": "text", "text": string(text)}},
		}, nil
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
	}
}

func toolAllowed(c *Caller, name string) bool {
	if c == nil {
		return false
	}
	for _, denied := range c.DeniedTools {
		if denied == name || denied == "*" {
			return false
		}
	}
	if len(c.AllowedTools) == 0 {
		return true
	}
	for _, allowed := range c.AllowedTools {
		if allowed == name || allowed == "*" {
			return true
		}
	}
	return false
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeRPC(w http.ResponseWriter, v rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
