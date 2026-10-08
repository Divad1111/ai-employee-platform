package acp

import (
	"encoding/json"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
)

func (s *StdioSession) resetUsage(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUsage = tokenusage.TokenUsage{
		Provider:    "cursor",
		Source:      "cursor_acp_event",
		UsageStatus: tokenusage.StatusPending,
	}
	s.promptText = prompt
	s.replyText = ""
}

func (s *StdioSession) rememberReply(reply string) {
	s.mu.Lock()
	s.replyText = reply
	s.mu.Unlock()
}

// LastUsage 兼容旧接口；无真实 usage 时返回 0，不再按字符估算。
func (s *StdioSession) LastUsage() Usage {
	u := s.LastTokenUsage()
	if u == nil {
		return Usage{Agent: "cursor", Source: "unavailable"}
	}
	return Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		Agent:        firstNonEmpty(u.Provider, s.agentModel, "cursor"),
		Source:       firstNonEmpty(u.Source, "cursor_acp_event"),
	}
}

// LastTokenUsage 返回本次 Send 的真实用量；未上报则为 UNAVAILABLE。
func (s *StdioSession) LastTokenUsage() *tokenusage.TokenUsage {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.lastUsage
	if u.Provider == "" {
		u.Provider = firstNonEmpty(s.agentModel, "cursor")
	}
	u.Normalize()
	if !u.HasAny() {
		u.UsageStatus = tokenusage.StatusUnavailable
		u.Source = "unavailable"
	} else if u.UsageStatus == tokenusage.StatusPending {
		u.UsageStatus = tokenusage.StatusFinal
	}
	out := u
	return &out
}

func (s *StdioSession) absorbUsage(raw json.RawMessage) {
	parsed, ok := parseTokenUsageFull(raw)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastUsage.InputTokens = mergeToken(s.lastUsage.InputTokens, parsed.InputTokens)
	s.lastUsage.OutputTokens = mergeToken(s.lastUsage.OutputTokens, parsed.OutputTokens)
	s.lastUsage.CachedInputTokens = mergeToken(s.lastUsage.CachedInputTokens, parsed.CachedInputTokens)
	s.lastUsage.CacheWriteInputTokens = mergeToken(s.lastUsage.CacheWriteInputTokens, parsed.CacheWriteInputTokens)
	s.lastUsage.CacheReadInputTokens = mergeToken(s.lastUsage.CacheReadInputTokens, parsed.CacheReadInputTokens)
	s.lastUsage.ReasoningOutputTokens = mergeToken(s.lastUsage.ReasoningOutputTokens, parsed.ReasoningOutputTokens)
	if parsed.TotalTokens > s.lastUsage.TotalTokens {
		s.lastUsage.TotalTokens = parsed.TotalTokens
	}
	if parsed.Provider != "" {
		s.agentModel = parsed.Provider
		s.lastUsage.Provider = parsed.Provider
	}
	s.lastUsage.Source = "cursor_acp_event"
	s.lastUsage.UsageStatus = tokenusage.StatusPartial
}

// Usage 兼容旧结构。
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	Agent        string
	Source       string
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

func parseTokenUsageFull(raw json.RawMessage) (tokenusage.TokenUsage, bool) {
	var out tokenusage.TokenUsage
	if len(raw) == 0 || string(raw) == "null" {
		return out, false
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return out, false
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
	ok := false
	for _, b := range blobs {
		if b == nil {
			continue
		}
		if a := stringField(b, "model", "agent", "modelId"); a != "" {
			out.Provider = a
		}
		if tok, okTok := b["tokens"].(map[string]any); okTok {
			fillUsageFromMap(&out, tok)
			if out.HasAny() {
				ok = true
			}
		}
		if usage, okU := b["usage"].(map[string]any); okU {
			fillUsageFromMap(&out, usage)
			if out.HasAny() {
				ok = true
			}
		}
		fillUsageFromMap(&out, b)
		if out.HasAny() {
			ok = true
		}
	}
	return out, ok
}

func fillUsageFromMap(u *tokenusage.TokenUsage, m map[string]any) {
	if v := firstInt(m, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens", "input"); v > 0 {
		u.InputTokens = v
	}
	if v := firstInt(m, "outputTokens", "output_tokens", "completionTokens", "completion_tokens", "output"); v > 0 {
		u.OutputTokens = v
	}
	if v := firstInt(m, "cachedInputTokens", "cached_input_tokens", "cacheReadTokens", "cache_read_tokens"); v > 0 {
		u.CachedInputTokens = v
	}
	if v := firstInt(m, "cacheWriteInputTokens", "cache_write_input_tokens", "cacheWriteTokens", "cache_write_tokens"); v > 0 {
		u.CacheWriteInputTokens = v
	}
	if v := firstInt(m, "cacheReadInputTokens", "cache_read_input_tokens"); v > 0 {
		u.CacheReadInputTokens = v
	}
	if v := firstInt(m, "reasoningOutputTokens", "reasoning_output_tokens", "reasoningTokens", "reasoning_tokens"); v > 0 {
		u.ReasoningOutputTokens = v
	}
	if v := firstInt(m, "totalTokens", "total_tokens"); v > 0 {
		u.TotalTokens = v
	}
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// normalizeMCPServers 把 MCP 配置转成 ACP session/new 接受的形状。
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
