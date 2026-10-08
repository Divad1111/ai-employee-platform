// Package tokenusage Center Server 侧任务真实 Token 用量。
package tokenusage

import "time"

const (
	StatusPending     = "PENDING"
	StatusPartial     = "PARTIAL"
	StatusFinal       = "FINAL"
	StatusUnavailable = "UNAVAILABLE"
)

// RunUsage 单次 Agent Run 明细。
type RunUsage struct {
	ID                    string    `json:"id"`
	JobID                 string    `json:"job_id"`
	WorkstationID         string    `json:"workstation_id"`
	DigitalEmployeeID     string    `json:"digital_employee_id"`
	Provider              string    `json:"provider"`
	ProviderSessionID     string    `json:"provider_session_id,omitempty"`
	ProviderRunID         string    `json:"provider_run_id,omitempty"`
	InputTokens           int64     `json:"input_tokens"`
	CachedInputTokens     int64     `json:"cached_input_tokens"`
	CacheWriteInputTokens int64     `json:"cache_write_input_tokens"`
	CacheReadInputTokens  int64     `json:"cache_read_input_tokens"`
	OutputTokens          int64     `json:"output_tokens"`
	ReasoningOutputTokens int64     `json:"reasoning_output_tokens"`
	TotalTokens           int64     `json:"total_tokens"`
	UsageStatus           string    `json:"usage_status"`
	UsageSource           string    `json:"usage_source"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// Summary 任务汇总。
type Summary struct {
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

// UpsertInput 幂等写入。
type UpsertInput struct {
	JobID                 string `json:"job_id"`
	WorkstationID         string `json:"workstation_id"`
	DigitalEmployeeID     string `json:"digital_employee_id"`
	Provider              string `json:"provider"`
	ProviderSessionID     string `json:"provider_session_id"`
	ProviderRunID         string `json:"provider_run_id"`
	InputTokens           int64  `json:"input_tokens"`
	CachedInputTokens     int64  `json:"cached_input_tokens"`
	CacheWriteInputTokens int64  `json:"cache_write_input_tokens"`
	CacheReadInputTokens  int64  `json:"cache_read_input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	ReasoningOutputTokens int64  `json:"reasoning_output_tokens"`
	TotalTokens           int64  `json:"total_tokens"`
	UsageStatus           string `json:"usage_status"`
	UsageSource           string `json:"usage_source"`
	IdempotencyKey        string `json:"idempotency_key"`
}

func (u *UpsertInput) Normalize() {
	if u.TotalTokens <= 0 {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
	if u.UsageStatus == "" {
		if u.InputTokens > 0 || u.OutputTokens > 0 || u.TotalTokens > 0 {
			u.UsageStatus = StatusFinal
		} else {
			u.UsageStatus = StatusUnavailable
		}
	}
	if u.UsageSource == "" {
		u.UsageSource = "unknown"
	}
}

// Aggregate 明细汇总。
func Aggregate(runs []*RunUsage) Summary {
	sum := Summary{UsageStatus: StatusUnavailable}
	if len(runs) == 0 {
		return sum
	}
	finals, partials, pendings, unavail := 0, 0, 0, 0
	for _, r := range runs {
		if r == nil {
			continue
		}
		sum.RunCount++
		sum.InputTokens += r.InputTokens
		sum.CachedInputTokens += r.CachedInputTokens
		sum.CacheWriteInputTokens += r.CacheWriteInputTokens
		sum.CacheReadInputTokens += r.CacheReadInputTokens
		sum.OutputTokens += r.OutputTokens
		sum.ReasoningOutputTokens += r.ReasoningOutputTokens
		sum.TotalTokens += r.TotalTokens
		switch r.UsageStatus {
		case StatusFinal:
			finals++
		case StatusPartial:
			partials++
		case StatusPending:
			pendings++
		default:
			unavail++
		}
	}
	switch {
	case pendings > 0 && finals+partials == 0:
		sum.UsageStatus = StatusPending
	case pendings > 0 || partials > 0 || (finals > 0 && unavail > 0):
		sum.UsageStatus = StatusPartial
	case finals == sum.RunCount:
		sum.UsageStatus = StatusFinal
	case unavail == sum.RunCount:
		sum.UsageStatus = StatusUnavailable
	default:
		sum.UsageStatus = StatusPartial
	}
	return sum
}
