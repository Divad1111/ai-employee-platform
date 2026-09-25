package acp

import (
	"encoding/json"
	"unicode/utf8"
)

// Usage 一次 prompt 的 token 消耗。
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	Agent        string
	Source       string // agent=协议上报，estimate=按文本估算
}

func (s *StdioSession) resetUsage(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inTok = 0
	s.outTok = 0
	s.promptText = prompt
	s.replyText = ""
}

func (s *StdioSession) rememberReply(reply string) {
	s.mu.Lock()
	s.replyText = reply
	s.mu.Unlock()
}

// LastUsage 返回本次 Send 的用量。Agent 没上报时按字符数估算。
func (s *StdioSession) LastUsage() Usage {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := Usage{
		InputTokens:  s.inTok,
		OutputTokens: s.outTok,
		Agent:        s.agentModel,
		Source:       "agent",
	}
	if u.Agent == "" {
		u.Agent = "cursor"
	}
	if u.InputTokens > 0 || u.OutputTokens > 0 {
		return u
	}
	u.Source = "estimate"
	u.InputTokens = estimateTokens(s.promptText)
	u.OutputTokens = estimateTokens(s.replyText)
	return u
}

func (s *StdioSession) absorbUsage(raw json.RawMessage) {
	in, out, agent, ok := parseTokenUsage(raw)
	if !ok && agent == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inTok = mergeToken(s.inTok, in)
	s.outTok = mergeToken(s.outTok, out)
	if agent != "" {
		s.agentModel = agent
	}
}

func mergeToken(cur, next int64) int64 {
	if next <= 0 {
		return cur
	}
	if cur == 0 || next >= cur {
		return next
	}
	return cur + next
}

// parseTokenUsage 从 ACP session/update 或 prompt 结果里取 token。
// 兼容 tokens.input/output、inputTokens/outputTokens、promptTokens/completionTokens。
func parseTokenUsage(raw json.RawMessage) (inTok, outTok int64, agent string, ok bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, 0, "", false
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return 0, 0, "", false
	}
	blobs := []map[string]any{root}
	if up, ok := root["update"].(map[string]any); ok {
		blobs = append(blobs, up)
	}
	if params, ok := root["params"].(map[string]any); ok {
		blobs = append(blobs, params)
		if up, ok := params["update"].(map[string]any); ok {
			blobs = append(blobs, up)
		}
	}
	for _, b := range blobs {
		if b == nil {
			continue
		}
		if a := stringField(b, "model", "agent", "modelId"); a != "" {
			agent = a
		}
		if tok, ok := b["tokens"].(map[string]any); ok {
			inTok = firstInt(tok, "input", "inputTokens", "prompt", "promptTokens")
			outTok = firstInt(tok, "output", "outputTokens", "completion", "completionTokens")
			if inTok > 0 || outTok > 0 {
				return inTok, outTok, agent, true
			}
		}
		inTok = firstInt(b, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens")
		outTok = firstInt(b, "outputTokens", "output_tokens", "completionTokens", "completion_tokens")
		if inTok > 0 || outTok > 0 {
			return inTok, outTok, agent, true
		}
	}
	return 0, 0, agent, false
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func firstInt(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch n := m[k].(type) {
		case float64:
			if n > 0 {
				return int64(n)
			}
		case json.Number:
			v, _ := n.Int64()
			if v > 0 {
				return v
			}
		}
	}
	return 0
}

func estimateTokens(text string) int64 {
	n := utf8.RuneCountInString(text)
	if n <= 0 {
		return 0
	}
	return int64(n)
}

// normalizeMCPServers 把 MCP 配置转成 ACP session/new 接受的形状。
// headers / env 必须是 {name,value} 数组；传对象时 Cursor Agent 会返回 -32603。
func normalizeMCPServers(in []any) []any {
	if len(in) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(in))
	for _, item := range in {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		cp := make(map[string]any, len(m))
		for k, v := range m {
			cp[k] = v
		}
		if v, ok := cp["headers"]; ok {
			if arr := toNameValueList(v); arr != nil {
				cp["headers"] = arr
			}
		}
		if v, ok := cp["env"]; ok {
			if arr := toNameValueList(v); arr != nil {
				cp["env"] = arr
			}
		}
		out = append(out, cp)
	}
	return out
}

func toNameValueList(v any) []map[string]string {
	switch m := v.(type) {
	case map[string]string:
		return nameValueFromMap(m)
	case map[string]any:
		flat := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				flat[k] = s
			}
		}
		return nameValueFromMap(flat)
	default:
		return nil
	}
}

func nameValueFromMap(m map[string]string) []map[string]string {
	if m == nil {
		return nil
	}
	out := make([]map[string]string, 0, len(m))
	for k, v := range m {
		out = append(out, map[string]string{"name": k, "value": v})
	}
	return out
}
