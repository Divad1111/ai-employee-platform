package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ai-employee-platform/server/internal/mcpauth"
	"net/url"
	"sort"
	"strings"
)

// ClientServer contains only settings understood by an external MCP client.
type ClientServer struct {
	BuiltinService string            `json:"-"`
	Type           string            `json:"type,omitempty"`
	URL            string            `json:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	Disabled       bool              `json:"disabled,omitempty"`
}

type ClientConfig struct {
	MCPServers map[string]*ClientServer `json:"mcpServers"`
}

// PrepareExport validates the entire registry before a new external token is
// issued. Credentials must belong to the caller even for global administrators.
func (s *Service) PrepareExport(ctx context.Context, userID, builtinURL, format string) (*ClientConfig, string, []string, error) {
	servers, err := s.store.ListServers(ctx)
	if err != nil {
		return nil, "", nil, err
	}
	result := &ClientConfig{MCPServers: map[string]*ClientServer{}}
	var builtinName string
	var warnings []string
	for _, srv := range servers {
		name := strings.TrimSpace(srv.Name)
		if name == "" {
			return nil, "", nil, fmt.Errorf("服务 %s 缺少名称，请先编辑服务", srv.ID)
		}
		if _, exists := result.MCPServers[name]; exists {
			return nil, "", nil, fmt.Errorf("服务名称 %s 重复，请先修改名称再导出", name)
		}
		entry := &ClientServer{Type: srv.Transport, Disabled: srv.Status != StatusActive}
		if entry.Type == "" {
			entry.Type = TransportHTTP
		}
		if builtin, ok := mcpauth.FindBuiltin(srv.ID); ok {
			if err := validateExportURL(builtinURL); err != nil {
				return nil, "", nil, fmt.Errorf("内置 MCP 地址无效，请配置 AIE_MCP_PUBLIC_URL")
			}
			entry.Type, entry.URL = TransportHTTP, mcpauth.Endpoint(builtinURL, builtin.Name)
			entry.BuiltinService = builtin.Name
			if builtin.Name == mcpauth.Workflow {
				builtinName = name
			}
		} else {
			if srv.ServerType == ServerTypeBuiltin {
				return nil, "", nil, fmt.Errorf("内置服务 %s 暂不支持导出", name)
			}
			switch entry.Type {
			case TransportHTTP, TransportSSE:
				if format == "codex" && entry.Type == TransportSSE {
					return nil, "", nil, fmt.Errorf("Codex 不支持 SSE 服务 %s，请使用 JSON 格式导出，或将服务改为 HTTP", name)
				}
				if err := validateExportURL(srv.Endpoint); err != nil {
					return nil, "", nil, fmt.Errorf("服务 %s 必须配置完整的 HTTP(S) 地址", name)
				}
				entry.URL = srv.Endpoint
				entry.Headers, err = exportStringMap(srv.Config, "headers")
			case TransportStdio:
				entry.Command = srv.Endpoint
				if command, ok := srv.Config["command"].(string); ok && strings.TrimSpace(command) != "" {
					entry.Command = command
				}
				if strings.TrimSpace(entry.Command) == "" {
					return nil, "", nil, fmt.Errorf("服务 %s 缺少启动命令", name)
				}
				entry.Type = ""
				entry.Env, err = exportStringMap(srv.Config, "env")
				if err == nil {
					entry.Args, err = exportStringList(srv.Config, "args")
				}
			default:
				return nil, "", nil, fmt.Errorf("服务 %s 的协议不支持导出", name)
			}
			if err != nil {
				return nil, "", nil, fmt.Errorf("服务 %s：%w", name, err)
			}
			credentialID, err := s.exportCredential(ctx, srv, userID)
			if err != nil {
				return nil, "", nil, fmt.Errorf("服务 %s：%w", name, err)
			}
			if credentialID != "" {
				// Repeat the ownership check for explicit and inferred selections.
				if err := s.CredentialForOwner(ctx, credentialID, userID); err != nil {
					return nil, "", nil, fmt.Errorf("服务 %s：只能导出本人拥有的凭证，请编辑服务选择导出凭证", name)
				}
				authn, err := s.resolveAuthentication(ctx, credentialID, srv.Transport, "USER", userID, "mcp_export")
				if err != nil {
					return nil, "", nil, fmt.Errorf("服务 %s 的导出凭证不可用，请检查凭证状态和认证设置", name)
				}
				if entry.Headers == nil {
					entry.Headers = map[string]string{}
				}
				for key, value := range authn.Headers {
					for existing := range entry.Headers {
						if strings.EqualFold(existing, key) {
							delete(entry.Headers, existing)
						}
					}
					entry.Headers[key] = value
				}
				if entry.Env == nil {
					entry.Env = map[string]string{}
				}
				for key, value := range authn.Env {
					entry.Env[key] = value
				}
			}
		}
		if entry.Disabled {
			warnings = append(warnings, fmt.Sprintf("%s 在平台中已停用，导出文件已标记停用，请检查目标客户端的启用状态", name))
		}
		if entry.Command != "" {
			warnings = append(warnings, fmt.Sprintf("%s 使用本地命令，目标电脑需安装对应程序并配置有效路径", name))
		} else if u, err := url.Parse(entry.URL); err == nil && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1") {
			warnings = append(warnings, fmt.Sprintf("%s 使用本机地址，其他电脑需要改成可访问的服务地址", name))
		}
		result.MCPServers[name] = entry
	}
	return result, builtinName, warnings, nil
}

func (s *Service) exportCredential(ctx context.Context, srv *MCPServer, userID string) (string, error) {
	if raw, exists := srv.Config["export_credential_id"]; exists {
		id, ok := raw.(string)
		if !ok {
			return "", fmt.Errorf("导出凭证配置无效，请编辑服务")
		}
		// An explicit empty value means use only the server's configured headers/env.
		return id, nil
	}
	bindings, err := s.store.ListBindingsByServer(ctx, srv.ID)
	if err != nil {
		return "", err
	}
	ids := map[string]bool{}
	hasCredentials := false
	for _, binding := range bindings {
		if !binding.Enabled || binding.CredentialID == "" {
			continue
		}
		hasCredentials = true
		if s.CredentialForOwner(ctx, binding.CredentialID, userID) == nil {
			ids[binding.CredentialID] = true
		}
	}
	if len(ids) > 1 {
		return "", fmt.Errorf("存在多个本人凭证，请编辑服务选择导出凭证")
	}
	for id := range ids {
		return id, nil
	}
	if hasCredentials {
		return "", fmt.Errorf("没有可导出的本人凭证，请编辑服务选择本人凭证，或选择仅使用服务配置")
	}
	return "", nil
}

func validateExportURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("无效 HTTP(S) URL")
	}
	return nil
}

func exportStringMap(config map[string]any, key string) (map[string]string, error) {
	raw, ok := config[key]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%s 必须是字符串键值对象", key)
	}
	var input map[string]any
	if err := json.Unmarshal(b, &input); err != nil {
		return nil, fmt.Errorf("%s 必须是字符串键值对象", key)
	}
	values := make(map[string]string, len(input))
	for name, raw := range input {
		value, ok := raw.(string)
		if !ok {
			return nil, fmt.Errorf("%s 必须是字符串键值对象", key)
		}
		if name == "" || strings.ContainsAny(name, "\r\n\x00") || strings.ContainsRune(value, '\x00') ||
			(key == "headers" && strings.ContainsAny(value, "\r\n")) {
			return nil, fmt.Errorf("%s 包含无效字符", key)
		}
		values[name] = value
	}
	return values, nil
}

func exportStringList(config map[string]any, key string) ([]string, error) {
	raw, ok := config[key]
	if !ok || raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("%s 必须是字符串数组", key)
	}
	var input []any
	if err := json.Unmarshal(b, &input); err != nil {
		return nil, fmt.Errorf("%s 必须是字符串数组", key)
	}
	values := make([]string, 0, len(input))
	for _, raw := range input {
		value, ok := raw.(string)
		if !ok || strings.ContainsRune(value, '\x00') {
			return nil, fmt.Errorf("%s 必须是有效字符串数组", key)
		}
		values = append(values, value)
	}
	return values, nil
}

// RenderExport returns either the requested mcpServers JSON or Codex TOML.
func RenderExport(config *ClientConfig, format string) (string, string, error) {
	if format == "json" {
		b, err := json.MarshalIndent(config, "", "  ")
		return string(b) + "\n", "mcp.json", err
	}
	if format != "codex" {
		return "", "", fmt.Errorf("不支持的导出格式")
	}
	quote := func(value string) string {
		b, _ := json.Marshal(value)
		return string(b)
	}
	var out strings.Builder
	out.WriteString("# Merge these MCP entries into ~/.codex/config.toml.\n")
	names := make([]string, 0, len(config.MCPServers))
	for name := range config.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := config.MCPServers[name]
		table := "mcp_servers." + quote(name)
		fmt.Fprintf(&out, "\n[%s]\n", table)
		if entry.Disabled {
			out.WriteString("enabled = false\n")
		}
		values := entry.Headers
		childTable := "http_headers"
		if entry.Command != "" {
			fmt.Fprintf(&out, "command = %s\n", quote(entry.Command))
			if len(entry.Args) > 0 {
				args, _ := json.Marshal(entry.Args)
				fmt.Fprintf(&out, "args = %s\n", args)
			}
			values, childTable = entry.Env, "env"
		} else {
			fmt.Fprintf(&out, "url = %s\n", quote(entry.URL))
		}
		if len(values) > 0 {
			fmt.Fprintf(&out, "\n[%s.%s]\n", table, childTable)
			keys := make([]string, 0, len(values))
			for key := range values {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				fmt.Fprintf(&out, "%s = %s\n", quote(key), quote(values[key]))
			}
		}
	}
	return out.String(), "mcp.codex.toml", nil
}
