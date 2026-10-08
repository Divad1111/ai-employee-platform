package codexusage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
	codexusage "github.com/ai-employee-platform/workstation/internal/tokenusage/codex"
)

type fakeSession struct {
	usage *tokenusage.TokenUsage
}

func (f *fakeSession) LastTokenUsage() *tokenusage.TokenUsage { return f.usage }

func TestCodexProviderNormalAndUnknownIgnoredByModel(t *testing.T) {
	p := codexusage.New()
	if !p.Supports(tokenusage.RuntimeCodex) {
		t.Fatal("supports")
	}
	u, err := p.GetRunUsage(context.Background(), &tokenusage.AgentExecution{
		ProviderSessionID: "th-1", ProviderRunID: "turn-1",
	}, &fakeSession{usage: &tokenusage.TokenUsage{
		InputTokens: 100, OutputTokens: 8, CachedInputTokens: 90, Source: "codex_cli_event",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if u.CachedInputTokens != 90 || u.TotalTokens != 108 || u.Provider != "codex" || u.ProviderRunID != "turn-1" {
		t.Fatalf("%+v", u)
	}
}

func TestCodexProviderUnavailable(t *testing.T) {
	p := codexusage.New()
	u, err := p.GetRunUsage(context.Background(), nil, &fakeSession{})
	if !errors.Is(err, tokenusage.ErrUsageNotAvailable) || u.HasAny() {
		t.Fatalf("%+v %v", u, err)
	}
}
