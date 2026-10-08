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
	u, ok := parseTokenUsageFull(raw)
	if !ok || u.InputTokens != 120 || u.OutputTokens != 45 || u.Provider != "composer" {
		t.Fatalf("%+v ok=%v", u, ok)
	}
}

func TestParseTokenUsageNoEstimate(t *testing.T) {
	u, ok := parseTokenUsageFull([]byte(`{"text":"hello world"}`))
	if ok || u.HasAny() {
		t.Fatalf("不应从纯文本估算: %+v ok=%v", u, ok)
	}
}
