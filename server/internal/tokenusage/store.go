package tokenusage

import "context"

// Store 明细持久化。
type Store interface {
	Upsert(ctx context.Context, u *RunUsage) (*RunUsage, bool, error)
	ListByJob(ctx context.Context, jobID string) ([]*RunUsage, error)
	GetByProviderRun(ctx context.Context, provider, runID string) (*RunUsage, error)
}
