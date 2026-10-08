// Package codexusage 从 Codex Runtime 读取真实 Token Usage。
package codexusage

import (
	"context"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
)

// Provider Codex TokenUsageProvider。
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() string { return "codex" }

func (p *Provider) Supports(runtime tokenusage.AgentRuntime) bool {
	return runtime == tokenusage.RuntimeCodex
}

func (p *Provider) GetRunUsage(_ context.Context, execution *tokenusage.AgentExecution, session any) (*tokenusage.TokenUsage, error) {
	if r, ok := session.(tokenusage.SessionReporter); ok {
		if u := r.LastTokenUsage(); u != nil {
			out := *u
			if out.Provider == "" {
				out.Provider = "codex"
			}
			if out.Source == "" {
				out.Source = "codex_cli_event"
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
		Provider:    "codex",
		Source:      "unavailable",
		UsageStatus: tokenusage.StatusUnavailable,
	}, tokenusage.ErrUsageNotAvailable
}
