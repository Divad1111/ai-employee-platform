package codex

import (
	"encoding/json"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
)

func notify(s *appServerSession, method string, payload any) {
	b, _ := json.Marshal(payload)
	s.onNotify(rpcMsg{Method: method, Params: b})
}

func TestCodexTurnCompletedUsage(t *testing.T) {
	s := newAppServerSession("s1", "codex", "/tmp", "")
	notify(s, "turn/completed", map[string]any{
		"turn": map[string]any{
			"status": "completed",
			"usage": map[string]any{
				"input_tokens":             24763,
				"cached_input_tokens":      24448,
				"cache_write_input_tokens": 1000,
				"output_tokens":            122,
				"reasoning_output_tokens":  80,
				"future_unknown_field":     "ignore-me",
			},
		},
	})
	u := s.LastTokenUsage()
	if u.InputTokens != 24763 || u.CachedInputTokens != 24448 || u.CacheWriteInputTokens != 1000 {
		t.Fatalf("%+v", u)
	}
	if u.OutputTokens != 122 || u.ReasoningOutputTokens != 80 {
		t.Fatalf("%+v", u)
	}
	if u.TotalTokens != 24763+122 {
		t.Fatalf("total 不应把 cached 再加一遍: %d", u.TotalTokens)
	}
	if u.UsageStatus != tokenusage.StatusFinal || u.Source != "codex_cli_event" {
		t.Fatalf("%+v", u)
	}
}

func TestCodexMissingOptionalFields(t *testing.T) {
	s := newAppServerSession("s1", "codex", "/tmp", "")
	notify(s, "thread/tokenUsage/updated", map[string]any{
		"inputTokens": 10, "outputTokens": 2,
	})
	u := s.LastTokenUsage()
	if u.InputTokens != 10 || u.OutputTokens != 2 || u.CachedInputTokens != 0 || u.ReasoningOutputTokens != 0 {
		t.Fatalf("%+v", u)
	}
	if u.UsageStatus != tokenusage.StatusPartial {
		t.Fatalf("中途事件应为 PARTIAL: %+v", u)
	}
}

func TestCodexDuplicateTurnDoesNotDouble(t *testing.T) {
	s := newAppServerSession("s1", "codex", "/tmp", "")
	payload := map[string]any{"usage": map[string]any{"input_tokens": 50, "output_tokens": 5}}
	notify(s, "turn/completed", payload)
	notify(s, "turn/completed", payload)
	u := s.LastTokenUsage()
	if u.InputTokens != 50 || u.OutputTokens != 5 || u.TotalTokens != 55 {
		t.Fatalf("重复事件被累加: %+v", u)
	}
}

func TestCodexMultipleTurnsLastWinsWithoutAdding(t *testing.T) {
	s := newAppServerSession("s1", "codex", "/tmp", "")
	notify(s, "thread/tokenUsage/updated", map[string]any{"input_tokens": 10, "output_tokens": 1})
	notify(s, "turn/completed", map[string]any{"usage": map[string]any{"input_tokens": 30, "output_tokens": 4, "total_tokens": 34}})
	u := s.LastTokenUsage()
	if u.InputTokens != 30 || u.OutputTokens != 4 || u.TotalTokens != 34 {
		t.Fatalf("%+v", u)
	}
}

func TestCodexV2NestedTokenUsageLastNotTotal(t *testing.T) {
	s := newAppServerSession("s1", "codex", "/tmp", "")
	notify(s, "thread/tokenUsage/updated", map[string]any{
		"threadId": "thr",
		"turnId":   "1",
		"tokenUsage": map[string]any{
			"last": map[string]any{
				"inputTokens": 5152, "cachedInputTokens": 3072,
				"outputTokens": 16, "reasoningOutputTokens": 0, "totalTokens": 5168,
			},
			"total": map[string]any{
				"inputTokens": 90000, "outputTokens": 8000, "totalTokens": 98000,
			},
		},
	})
	u := s.LastTokenUsage()
	if u.InputTokens != 5152 || u.OutputTokens != 16 || u.CachedInputTokens != 3072 || u.TotalTokens != 5168 {
		t.Fatalf("应取本回合 last，而不是线程累计 total: %+v", u)
	}
	notify(s, "turn/completed", map[string]any{"turn": map[string]any{"status": "completed"}})
	u = s.LastTokenUsage()
	if u.UsageStatus != tokenusage.StatusFinal || u.Source != "codex_cli_event" || u.InputTokens != 5152 {
		t.Fatalf("回合结束应升为 FINAL: %+v", u)
	}
}

func TestCodexNoUsageIsUnavailable(t *testing.T) {
	s := newAppServerSession("s1", "codex", "/tmp", "")
	notify(s, "turn/completed", map[string]any{"turn": map[string]any{"status": "completed"}})
	u := s.LastTokenUsage()
	if u.HasAny() || u.UsageStatus != tokenusage.StatusUnavailable {
		t.Fatalf("%+v", u)
	}
}
