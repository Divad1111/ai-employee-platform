package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestCursorMCPConfigurationReachesSessionNew(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	s := NewStdioSession("session", binary, []string{"-test.run=^TestACPMCPProtocolPeer$"}, t.TempDir())
	s.SetEnv([]string{"AIE_TEST_ACP_MCP_PEER=1"})
	s.SetMCPServers([]any{
		map[string]any{"name": "jira", "type": "http", "url": "http://localhost/mcp", "headers": map[string]string{"Authorization": "Token test-key"}},
		map[string]any{"name": "unity", "type": "stdio", "command": "unity-mcp", "env": map[string]string{"UNITY_TOKEN": "test-key"}},
		map[string]any{"name": "public", "type": "http", "url": "http://localhost/public"},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	defer s.Stop(context.Background())
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
}

// 用独立子进程验证完整 Start 握手，防止仅测试转换函数而漏掉真实下发链路。
func TestACPMCPProtocolPeer(t *testing.T) {
	if os.Getenv("AIE_TEST_ACP_MCP_PEER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var request struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		result := map[string]any{}
		if request.Method == "session/new" {
			servers, ok := request.Params["mcpServers"].([]any)
			valid := ok && len(servers) == 3
			if valid {
				http := servers[0].(map[string]any)
				headers, ok := http["headers"].([]any)
				valid = ok && len(headers) == 1
				if valid {
					h := headers[0].(map[string]any)
					valid = h["name"] == "Authorization" && h["value"] == "Token test-key"
				}
				stdio := servers[1].(map[string]any)
				env, envOK := stdio["env"].([]any)
				args, argsOK := stdio["args"].([]any)
				_, hasType := stdio["type"]
				valid = valid && envOK && len(env) == 1 && argsOK && len(args) == 0 && !hasType
				if envOK && len(env) == 1 {
					e := env[0].(map[string]any)
					valid = valid && e["name"] == "UNITY_TOKEN" && e["value"] == "test-key"
				}
				public := servers[2].(map[string]any)
				publicHeaders, headersOK := public["headers"].([]any)
				valid = valid && headersOK && len(publicHeaders) == 0
			}
			if !valid {
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32602, "message": "MCP authentication or protocol shape missing"}})
				os.Exit(1)
			}
			result["sessionId"] = "session-verified"
		}
		_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
		if request.Method == "session/new" {
			os.Exit(0)
		}
	}
	os.Exit(2)
}
