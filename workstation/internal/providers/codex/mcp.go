package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// codexMCPConfig 将平台的 MCPServerSpec 形状转换为 Codex 的 mcp_servers 配置。
// 配置通过 thread/start 传递，不写用户 config.toml，也不把凭证放进进程参数。
func codexMCPConfig(servers []any) (map[string]any, error) {
	result := make(map[string]any, len(servers))
	for _, item := range servers {
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, errors.New("MCP 配置无法序列化")
		}
		var spec struct {
			Name    string            `json:"name"`
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			Headers map[string]string `json:"headers"`
		}
		if err := json.Unmarshal(raw, &spec); err != nil {
			return nil, errors.New("MCP 配置格式无效")
		}
		if strings.TrimSpace(spec.Name) == "" {
			return nil, errors.New("MCP 服务名称不能为空")
		}
		if _, exists := result[spec.Name]; exists {
			return nil, errors.New("MCP 服务名称重复")
		}
		transport := spec.Type
		if transport == "" {
			transport = "http"
			if spec.Command != "" {
				transport = "stdio"
			}
		}
		config := map[string]any{}
		switch transport {
		case "http":
			if spec.URL == "" {
				return nil, errors.New("HTTP MCP 缺少 URL")
			}
			config["url"] = spec.URL
			if len(spec.Headers) > 0 {
				config["http_headers"] = spec.Headers
			}
		case "stdio":
			if spec.Command == "" {
				return nil, errors.New("STDIO MCP 缺少启动命令")
			}
			config["command"] = spec.Command
			if len(spec.Args) > 0 {
				config["args"] = spec.Args
			}
			if len(spec.Env) > 0 {
				config["env"] = spec.Env
			}
		case "sse":
			return nil, errors.New("Codex 不支持旧版 SSE MCP，请改用 Streamable HTTP")
		default:
			return nil, fmt.Errorf("Codex 不支持 MCP 传输类型 %q", transport)
		}
		result[spec.Name] = config
	}
	return result, nil
}
