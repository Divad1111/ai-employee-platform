package acp

import "testing"

func TestNormalizeMCPHeaders(t *testing.T) {
	got := normalizeMCPServers([]any{
		map[string]any{
			"name": "workflow-mcp", "type": "http", "url": "http://127.0.0.1/mcp",
			"headers": map[string]string{"Authorization": "Bearer x"},
		},
	})
	m := got[0].(map[string]any)
	hdrs := m["headers"].([]map[string]string)
	if len(hdrs) != 1 || hdrs[0]["name"] != "Authorization" || hdrs[0]["value"] != "Bearer x" {
		t.Fatal(hdrs)
	}
}

func TestParseTokenUsage(t *testing.T) {
	raw := []byte(`{"update":{"sessionUpdate":"usage_update","model":"composer","tokens":{"input":120,"output":45}}}`)
	in, out, agent, ok := parseTokenUsage(raw)
	if !ok || in != 120 || out != 45 || agent != "composer" {
		t.Fatalf("%d %d %s %v", in, out, agent, ok)
	}
}
