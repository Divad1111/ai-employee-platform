// Package tokenusage 统一采集 Agent Runtime 真实 Token 用量。
// 禁止按文本长度或 tokenizer 估算冒充真实 usage。
package tokenusage

import "time"

// usage_status
const (
	StatusPending     = "PENDING"
	StatusPartial     = "PARTIAL"
	StatusFinal       = "FINAL"
	StatusUnavailable = "UNAVAILABLE"
)

// AgentRuntime 驱动引擎标识。
type AgentRuntime string

const (
	RuntimeCodex       AgentRuntime = "codex"
	RuntimeCursor      AgentRuntime = "cursor"
	RuntimeAntigravity AgentRuntime = "antigravity"
)

// TokenUsage 单次 Run 的真实用量。
type TokenUsage struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	CacheWriteInputTokens int64  `json:"cache_write_input_tokens"`
	CacheReadInputTokens  int64  `json:"cache_read_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	Provider              string `json:"provider"`
	ProviderSessionID     string `json:"provider_session_id,omitempty"`
	ProviderRunID         string `json:"provider_run_id,omitempty"`
	Source                string `json:"source"`
	UsageStatus           string `json:"usage_status"`
}

// Normalize 补全 TotalTokens 与默认状态。Cached 不重复计入 Total。
func (u *TokenUsage) Normalize() {
	if u == nil {
		return
	}
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	if u.UsageStatus == "" {
		if u.HasAny() {
			u.UsageStatus = StatusFinal
		} else {
			u.UsageStatus = StatusUnavailable
		}
	}
}

// HasAny 是否拿到任意真实计数。
func (u *TokenUsage) HasAny() bool {
	if u == nil {
		return false
	}
	return u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0 ||
		u.CachedInputTokens > 0 || u.ReasoningOutputTokens > 0 ||
		u.CacheWriteInputTokens > 0 || u.CacheReadInputTokens > 0
}

// TokenUsageSummary 任务级汇总。
type TokenUsageSummary struct {
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	CacheWriteInputTokens int64  `json:"cache_write_input_tokens"`
	CacheReadInputTokens  int64  `json:"cache_read_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	UsageStatus           string `json:"usage_status"`
	RunCount              int    `json:"run_count"`
}

// AgentExecution 一次 Agent Run 的追溯上下文。
type AgentExecution struct {
	ID                string
	TaskID            string
	DigitalEmployeeID string
	WorkstationID     string
	Runtime           AgentRuntime
	ProviderSessionID string
	ProviderRunID     string
	StartedAt         time.Time
	FinishedAt        *time.Time
}
