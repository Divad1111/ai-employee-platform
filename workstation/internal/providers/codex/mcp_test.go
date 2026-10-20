package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// mcpRPCPeer 模拟 app-server 对端，检查真正发出的 JSON-RPC 请求。
type mcpRPCPeer struct {
	s        *appServerSession
	requests []map[string]any
}

func (p *mcpRPCPeer) Close() error { return nil }
func (p *mcpRPCPeer) Write(raw []byte) (int, error) {
	var request map[string]any
	if err := json.Unmarshal(raw, &request); err != nil {
		return 0, err
	}
	p.requests = append(p.requests, request)
	id := int(request["id"].(float64))
	var response any
	switch request["method"] {
	case "thread/start":
		response = map[string]any{"thread": map[string]any{"id": "thread-mcp"}}
	case "turn/start":
		response = map[string]any{"turn": map[string]any{"id": "turn-mcp"}}
	default:
		return 0, fmt.Errorf("unexpected method")
	}
	body, _ := json.Marshal(response)
	p.s.mu.Lock()
	ch := p.s.pending[id]
	delete(p.s.pending, id)
	p.s.mu.Unlock()
	ch <- rpcMsg{ID: id, Result: body}
	if request["method"] == "turn/start" {
		body, _ := json.Marshal(map[string]any{"turn": map[string]any{"status": "completed"}})
		p.s.onNotify(rpcMsg{Method: "turn/completed", Params: body})
	}
	return len(raw), nil
}

func TestCodexSendsMCPConfigurationToThread(t *testing.T) {
	s := newAppServerSession("session", "unused", t.TempDir(), "test-model")
	s.SetMCPServers([]any{
		map[string]any{"name": "jira", "type": "http", "url": "http://localhost/mcp", "headers": map[string]string{"Authorization": "Token test-key", "X-Project": "A"}},
		map[string]any{"name": "unity", "type": "stdio", "command": "unity-mcp", "args": []string{"--stdio"}, "env": map[string]string{"UNITY_KEY": "test-key"}},
	})
	s.ready = true
	peer := &mcpRPCPeer{s: s}
	s.stdin = peer
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for i := 0; i < 2; i++ {
		if _, err := s.Send(ctx, []byte("hello")); err != nil {
			t.Fatal(err)
		}
	}
	if len(peer.requests) != 3 {
		t.Fatal("existing thread should be reused")
	}
	params := peer.requests[0]["params"].(map[string]any)
	if params["model"] != "test-model" {
		t.Fatal("model configuration lost")
	}
	servers := params["config"].(map[string]any)["mcp_servers"].(map[string]any)
	httpServer := servers["jira"].(map[string]any)
	if httpServer["url"] != "http://localhost/mcp" {
		t.Fatal("MCP URL lost")
	}
	headers := httpServer["http_headers"].(map[string]any)
	if headers["Authorization"] != "Token test-key" || headers["X-Project"] != "A" {
		t.Fatal("MCP headers lost")
	}
	stdio := servers["unity"].(map[string]any)
	if stdio["command"] != "unity-mcp" || stdio["env"].(map[string]any)["UNITY_KEY"] != "test-key" || stdio["args"].([]any)[0] != "--stdio" {
		t.Fatal("STDIO configuration lost")
	}
	if _, exists := httpServer["headers"]; exists {
		t.Fatal("ACP header shape leaked into Codex config")
	}
}

func TestCodexRejectsUnsupportedMCPConfiguration(t *testing.T) {
	for _, servers := range [][]any{
		{map[string]any{"name": "old", "type": "sse", "url": "http://localhost/sse"}},
		{map[string]any{"name": "invalid", "type": "stdio"}},
		{map[string]any{"name": "invalid", "type": "http"}},
		{map[string]any{"url": "http://localhost/mcp"}},
		{map[string]any{"name": "same", "url": "http://localhost/a"}, map[string]any{"name": "same", "url": "http://localhost/b"}},
	} {
		if _, err := codexMCPConfig(servers); err == nil {
			t.Fatal("invalid MCP configuration accepted")
		}
	}
}
