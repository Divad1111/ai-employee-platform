package cursorusage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
	cursorusage "github.com/ai-employee-platform/workstation/internal/tokenusage/cursor"
)

type fakeSession struct {
	usage *tokenusage.TokenUsage
	err   error
}

func (f *fakeSession) LastTokenUsage() *tokenusage.TokenUsage {
	if f.err != nil {
		return nil
	}
	return f.usage
}

func TestCursorNormalUsage(t *testing.T) {
	p := cursorusage.New()
	if !p.Supports(tokenusage.RuntimeCursor) || p.Supports(tokenusage.RuntimeCodex) {
		t.Fatal("supports")
	}
	u, err := p.GetRunUsage(context.Background(), &tokenusage.AgentExecution{
		Runtime: tokenusage.RuntimeCursor, ProviderRunID: "run-c",
	}, &fakeSession{usage: &tokenusage.TokenUsage{
		InputTokens: 12, OutputTokens: 3, CacheReadInputTokens: 4, TotalTokens: 15,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if u.InputTokens != 12 || u.OutputTokens != 3 || u.TotalTokens != 15 || u.Source != "cursor_acp_event" {
		t.Fatalf("%+v", u)
	}
	if u.UsageStatus != tokenusage.StatusFinal || u.ProviderRunID != "run-c" {
		t.Fatalf("%+v", u)
	}
}

func TestCursorRunMissing(t *testing.T) {
	p := cursorusage.New()
	u, err := p.GetRunUsage(context.Background(), &tokenusage.AgentExecution{Runtime: tokenusage.RuntimeCursor}, nil)
	if !errors.Is(err, tokenusage.ErrUsageNotAvailable) {
		t.Fatalf("err=%v", err)
	}
	if u.UsageStatus != tokenusage.StatusUnavailable || u.HasAny() {
		t.Fatalf("%+v", u)
	}
}

func TestCursorUsageDelayedThenAvailable(t *testing.T) {
	p := cursorusage.New()
	sess := &fakeSession{}
	u, err := p.GetRunUsage(context.Background(), &tokenusage.AgentExecution{ProviderRunID: "late"}, sess)
	if !errors.Is(err, tokenusage.ErrUsageNotAvailable) || u.UsageStatus != tokenusage.StatusUnavailable {
		t.Fatalf("delay: %+v %v", u, err)
	}
	sess.usage = &tokenusage.TokenUsage{InputTokens: 9, OutputTokens: 1}
	u, err = p.GetRunUsage(context.Background(), &tokenusage.AgentExecution{ProviderRunID: "late"}, sess)
	if err != nil || u.InputTokens != 9 || u.UsageStatus != tokenusage.StatusFinal {
		t.Fatalf("later: %+v %v", u, err)
	}
}

func TestCursorTemporaryFailure(t *testing.T) {
	p := cursorusage.New()
	sess := &fakeSession{err: errors.New("temporary")}
	u, err := p.GetRunUsage(context.Background(), nil, sess)
	if !errors.Is(err, tokenusage.ErrUsageNotAvailable) {
		t.Fatal(err)
	}
	if u.UsageStatus != tokenusage.StatusUnavailable {
		t.Fatalf("%+v", u)
	}
}
