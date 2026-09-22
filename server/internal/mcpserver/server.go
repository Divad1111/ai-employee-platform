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
	Kind       string // employee | user | admin_session
	SubjectID  string
	Scope      string // READ | ADMIN
	CanWrite   bool
	EmployeeScope *workflowmcp.Scope // employee 时非空
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
	WF      *workflowmcp.Service
	MCPAuth *mcpauth.Service
	Auth    *auth.Service
	Syncer  SkillSyncer // 可选：同步技能到工作站
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
				"name": "workflow-mcp", "version": "1.0.0",
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

		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeRPC(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
			return
		}
		if req.JSONRPC == "" {
			req.JSONRPC = "2.0"
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
		c := &Caller{SubjectID: t.SubjectID, Scope: t.Scope}
		if t.SubjectType == mcpauth.SubjectEmployee {
			c.Kind = "employee"
			c.CanWrite = false
			if s.WF != nil {
				scope, _ := s.WF.ResolveScope(r.Context(), t.SubjectID)
				c.EmployeeScope = scope
			}
		} else {
			c.Kind = "user"
			c.CanWrite = t.Scope == mcpauth.ScopeAdmin
			if s.Auth != nil {
				// 用用户角色再验一次写权限
				// 简化：ADMIN scope 即可写
			}
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
	canWrite := s.Auth.Authorize(r.Context(), sess, "workflow.write") == nil
	return &Caller{
		Kind: "admin_session", SubjectID: sess.UserID,
		Scope: mcpauth.ScopeAdmin, CanWrite: canWrite,
	}, nil
}

func (s *Server) dispatch(ctx context.Context, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "workflow-mcp", "version": "1.0.0"},
		}, nil
	case "notifications/initialized", "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefs()}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{Code: -32602, Message: "invalid params"}
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
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
