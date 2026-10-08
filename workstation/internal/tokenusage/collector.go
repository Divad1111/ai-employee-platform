package tokenusage

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// RunRecord 一次执行的持久化视图。
type RunRecord struct {
	Execution *AgentExecution
	Usage     *TokenUsage
}

// Collector 采集、保存 Run Usage，并在任务结束时汇总。
type Collector struct {
	mu      sync.Mutex
	byTask  map[string][]*RunRecord
	byExec  map[string]*RunRecord
	reg     *Registry
}

func NewCollector(reg *Registry) *Collector {
	return &Collector{
		byTask: map[string][]*RunRecord{},
		byExec: map[string]*RunRecord{},
		reg:    reg,
	}
}

func (c *Collector) Start(execution *AgentExecution) error {
	if execution == nil || execution.TaskID == "" {
		return fmt.Errorf("%w: 缺少 task_id", ErrExecutionNotFound)
	}
	if execution.ID == "" {
		execution.ID = fmt.Sprintf("exec-%s-%d", execution.TaskID, time.Now().UnixNano())
	}
	if execution.StartedAt.IsZero() {
		execution.StartedAt = time.Now().UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rec := &RunRecord{Execution: execution, Usage: &TokenUsage{
		Provider: string(execution.Runtime),
		ProviderSessionID: execution.ProviderSessionID,
		ProviderRunID: execution.ProviderRunID,
		UsageStatus: StatusPending,
		Source: "",
	}}
	c.byExec[execution.ID] = rec
	c.byTask[execution.TaskID] = append(c.byTask[execution.TaskID], rec)
	return nil
}

func (c *Collector) Record(_ context.Context, execution *AgentExecution, usage *TokenUsage) error {
	if execution == nil || execution.ID == "" {
		return ErrExecutionNotFound
	}
	if usage == nil {
		usage = &TokenUsage{UsageStatus: StatusUnavailable}
	}
	usage.Normalize()
	if usage.Provider == "" {
		usage.Provider = string(execution.Runtime)
	}
	if usage.ProviderSessionID == "" {
		usage.ProviderSessionID = execution.ProviderSessionID
	}
	if usage.ProviderRunID == "" {
		usage.ProviderRunID = execution.ProviderRunID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, ok := c.byExec[execution.ID]
	if !ok {
		return ErrExecutionNotFound
	}
	rec.Usage = usage
	now := time.Now().UTC()
	rec.Execution.FinishedAt = &now
	return nil
}

// CaptureFromSession 用 Registry Provider 从会话读取真实 usage 并 Record。
func (c *Collector) CaptureFromSession(ctx context.Context, execution *AgentExecution, session any) (*TokenUsage, error) {
	if c.reg == nil || execution == nil {
		return nil, ErrProviderNotSupported
	}
	p, err := c.reg.Get(execution.Runtime)
	if err != nil {
		u := &TokenUsage{
			Provider:    string(execution.Runtime),
			UsageStatus: StatusUnavailable,
			Source:      "unavailable",
		}
		_ = c.Record(ctx, execution, u)
		return u, err
	}
	u, err := p.GetRunUsage(ctx, execution, session)
	if err != nil || u == nil || !u.HasAny() {
		u = &TokenUsage{
			Provider:          string(execution.Runtime),
			ProviderSessionID: execution.ProviderSessionID,
			ProviderRunID:     execution.ProviderRunID,
			UsageStatus:       StatusUnavailable,
			Source:            "unavailable",
		}
		_ = c.Record(ctx, execution, u)
		if err == nil {
			err = ErrUsageNotAvailable
		}
		return u, err
	}
	u.Normalize()
	_ = c.Record(ctx, execution, u)
	return u, nil
}

func (c *Collector) Finalize(taskID string) (*TokenUsageSummary, []*RunRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()
	recs := c.byTask[taskID]
	usages := make([]*TokenUsage, 0, len(recs))
	for _, r := range recs {
		if r != nil && r.Usage != nil {
			usages = append(usages, r.Usage)
		}
	}
	return Aggregate(usages), append([]*RunRecord(nil), recs...)
}

func (c *Collector) Runs(taskID string) []*RunRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*RunRecord(nil), c.byTask[taskID]...)
}
