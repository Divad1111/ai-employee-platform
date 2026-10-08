package tokenusage

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// JobSummaryWriter 把汇总写回 jobs 表缓存字段。
type JobSummaryWriter interface {
	ApplyTokenSummary(ctx context.Context, jobID string, sum Summary, agent, source string) (createdBy string, err error)
}

// Service Token Usage 领域服务。
type Service struct {
	store Store
	jobs  JobSummaryWriter
}

func NewService(store Store, jobs JobSummaryWriter) *Service {
	return &Service{store: store, jobs: jobs}
}

func (s *Service) Upsert(ctx context.Context, in UpsertInput) (*RunUsage, Summary, bool, error) {
	if strings.TrimSpace(in.JobID) == "" {
		return nil, Summary{}, false, fmt.Errorf("需要 job_id")
	}
	in.Normalize()
	if in.ProviderRunID == "" && in.IdempotencyKey != "" {
		in.ProviderRunID = in.IdempotencyKey
	}
	if in.ProviderRunID == "" {
		in.ProviderRunID = fmt.Sprintf("%s-%s-%s", in.JobID, in.Provider, in.ProviderSessionID)
	}
	run := &RunUsage{
		JobID:                 in.JobID,
		WorkstationID:         in.WorkstationID,
		DigitalEmployeeID:     in.DigitalEmployeeID,
		Provider:              in.Provider,
		ProviderSessionID:     in.ProviderSessionID,
		ProviderRunID:         in.ProviderRunID,
		InputTokens:           in.InputTokens,
		CachedInputTokens:     in.CachedInputTokens,
		CacheWriteInputTokens: in.CacheWriteInputTokens,
		CacheReadInputTokens:  in.CacheReadInputTokens,
		OutputTokens:          in.OutputTokens,
		ReasoningOutputTokens: in.ReasoningOutputTokens,
		TotalTokens:           in.TotalTokens,
		UsageStatus:           in.UsageStatus,
		UsageSource:           in.UsageSource,
	}
	saved, created, err := s.store.Upsert(ctx, run)
	if err != nil {
		return nil, Summary{}, false, err
	}
	sum, err := s.RefreshJobSummary(ctx, in.JobID, in.Provider, in.UsageSource)
	if err != nil {
		return saved, sum, created, err
	}
	return saved, sum, created, nil
}

func (s *Service) RefreshJobSummary(ctx context.Context, jobID, agent, source string) (Summary, error) {
	runs, err := s.store.ListByJob(ctx, jobID)
	if err != nil {
		return Summary{}, err
	}
	sum := Aggregate(runs)
	if s.jobs != nil {
		if _, err := s.jobs.ApplyTokenSummary(ctx, jobID, sum, agent, source); err != nil {
			return sum, err
		}
	}
	return sum, nil
}

func (s *Service) GetJobUsage(ctx context.Context, jobID string) (Summary, []*RunUsage, error) {
	runs, err := s.store.ListByJob(ctx, jobID)
	if err != nil {
		return Summary{}, nil, err
	}
	return Aggregate(runs), runs, nil
}

// UpsertFromEventPayload 从工作站事件 payload 写入（Center 不解析 Provider 细节）。
func (s *Service) UpsertFromEventPayload(ctx context.Context, jobID, wsID, empID string, payload map[string]string) (createdBy string, first bool, err error) {
	if s == nil {
		return "", false, nil
	}
	parseI64 := func(k string) int64 {
		v, _ := strconv.ParseInt(payload[k], 10, 64)
		return v
	}
	src := payload["usage_source"]
	if src == "" {
		src = payload["token_source"]
	}
	status := payload["usage_status"]
	inTok := parseI64("input_tokens")
	outTok := parseI64("output_tokens")
	totalTok := parseI64("total_tokens")
	// 旧「按文本估算」一律丢弃数值，标记 UNAVAILABLE，不可冒充真实 usage。
	if src == "estimate" {
		src = "unavailable"
		status = StatusUnavailable
		inTok, outTok, totalTok = 0, 0, 0
	}
	if status == "" {
		if src == "unavailable" || (inTok == 0 && outTok == 0 && totalTok == 0) {
			status = StatusUnavailable
		} else {
			status = StatusFinal
		}
	}
	provider := payload["provider"]
	if provider == "" {
		provider = payload["agent"]
	}
	runID := payload["provider_run_id"]
	if runID == "" {
		runID = jobID + "-" + payload["session_id"]
	}
	in := UpsertInput{
		JobID:                 jobID,
		WorkstationID:         wsID,
		DigitalEmployeeID:     empID,
		Provider:              provider,
		ProviderSessionID:     payload["provider_session_id"],
		ProviderRunID:         runID,
		InputTokens:           inTok,
		CachedInputTokens:     parseI64("cached_input_tokens"),
		CacheWriteInputTokens: parseI64("cache_write_input_tokens"),
		CacheReadInputTokens:  parseI64("cache_read_input_tokens"),
		OutputTokens:          outTok,
		ReasoningOutputTokens: parseI64("reasoning_output_tokens"),
		TotalTokens:           totalTok,
		UsageStatus:           status,
		UsageSource:           src,
	}
	if in.ProviderSessionID == "" {
		in.ProviderSessionID = payload["session_id"]
	}
	_, _, created, err := s.Upsert(ctx, in)
	if err != nil {
		return "", false, err
	}
	return "", created, nil
}
