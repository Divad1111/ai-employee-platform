// Package cursorusage 从 Cursor ACP Runtime 读取真实 Token Usage。
package cursorusage

import (
	"context"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
)

// Provider Cursor TokenUsageProvider。
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "cursor" }

func (p *Provider) Supports(runtime tokenusage.AgentRuntime) bool {
	return runtime == tokenusage.RuntimeCursor
}

func (p *Provider) GetRunUsage(_ context.Context, execution *tokenusage.AgentExecution, session any) (*tokenusage.TokenUsage, error) {
	if r, ok := session.(tokenusage.SessionReporter); ok {
		if u := r.LastTokenUsage(); u != nil {
			out := *u
			if out.Provider == "" {
				out.Provider = "cursor"
			}
			if out.Source == "" {
				out.Source = "cursor_acp_event"
			}
			if execution != nil {
				if out.ProviderSessionID == "" {
					out.ProviderSessionID = execution.ProviderSessionID
				}
				if out.ProviderRunID == "" {
					out.ProviderRunID = execution.ProviderRunID
				}
			}
			out.Normalize()
			if !out.HasAny() {
				out.UsageStatus = tokenusage.StatusUnavailable
				return &out, tokenusage.ErrUsageNotAvailable
			}
			if out.UsageStatus == "" || out.UsageStatus == tokenusage.StatusUnavailable {
				out.UsageStatus = tokenusage.StatusFinal
			}
			return &out, nil
		}
	}
	return &tokenusage.TokenUsage{
		Provider:    "cursor",
		Source:      "unavailable",
		UsageStatus: tokenusage.StatusUnavailable,
	}, tokenusage.ErrUsageNotAvailable
}
