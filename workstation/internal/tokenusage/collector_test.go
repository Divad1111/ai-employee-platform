package tokenusage_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
	codexusage "github.com/ai-employee-platform/workstation/internal/tokenusage/codex"
	cursorusage "github.com/ai-employee-platform/workstation/internal/tokenusage/cursor"
)

type reportSess struct {
	u *tokenusage.TokenUsage
}

func (s *reportSess) LastTokenUsage() *tokenusage.TokenUsage { return s.u }

func TestCollectorRetryKeepsBothRuns(t *testing.T) {
	reg := tokenusage.NewRegistry()
	reg.Register(codexusage.New())
	c := tokenusage.NewCollector(reg)
	ctx := context.Background()

	run1 := &tokenusage.AgentExecution{ID: "exec-1", TaskID: "JOB-R", Runtime: tokenusage.RuntimeCodex, ProviderRunID: "run-fail"}
	run2 := &tokenusage.AgentExecution{ID: "exec-2", TaskID: "JOB-R", Runtime: tokenusage.RuntimeCodex, ProviderRunID: "run-ok"}
	if err := c.Start(run1); err != nil {
		t.Fatal(err)
	}
	if err := c.Start(run2); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CaptureFromSession(ctx, run1, &reportSess{u: &tokenusage.TokenUsage{
		InputTokens: 10, OutputTokens: 1, UsageStatus: tokenusage.StatusFinal, Source: "codex_cli_event",
	}}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CaptureFromSession(ctx, run2, &reportSess{u: &tokenusage.TokenUsage{
		InputTokens: 40, OutputTokens: 4, UsageStatus: tokenusage.StatusFinal, Source: "codex_cli_event",
	}}); err != nil {
		t.Fatal(err)
	}
	sum, recs := c.Finalize("JOB-R")
	if len(recs) != 2 {
		t.Fatalf("重试覆盖了历史 Run: %d", len(recs))
	}
	if sum.InputTokens != 50 || sum.OutputTokens != 5 || sum.UsageStatus != tokenusage.StatusFinal {
		t.Fatalf("%+v", sum)
	}
}

func TestCollectorPartialThenFinal(t *testing.T) {
	reg := tokenusage.NewRegistry()
	reg.Register(cursorusage.New())
	c := tokenusage.NewCollector(reg)
	ex := &tokenusage.AgentExecution{ID: "e1", TaskID: "JOB-P", Runtime: tokenusage.RuntimeCursor, ProviderRunID: "r"}
	_ = c.Start(ex)
	sum, _ := c.Finalize("JOB-P")
	if sum.UsageStatus != tokenusage.StatusPending {
		t.Fatalf("start 后应为 PENDING: %+v", sum)
	}
	if err := c.Record(context.Background(), ex, &tokenusage.TokenUsage{
		InputTokens: 7, OutputTokens: 1, UsageStatus: tokenusage.StatusPartial, Source: "cursor_acp_event",
	}); err != nil {
		t.Fatal(err)
	}
	sum, _ = c.Finalize("JOB-P")
	if sum.UsageStatus != tokenusage.StatusPartial || sum.InputTokens != 7 {
		t.Fatalf("%+v", sum)
	}
	if err := c.Record(context.Background(), ex, &tokenusage.TokenUsage{
		InputTokens: 7, OutputTokens: 2, UsageStatus: tokenusage.StatusFinal, Source: "cursor_acp_event",
	}); err != nil {
		t.Fatal(err)
	}
	sum, recs := c.Finalize("JOB-P")
	if len(recs) != 1 || sum.UsageStatus != tokenusage.StatusFinal || sum.OutputTokens != 2 {
		t.Fatalf("更新同一 Run 不应新增: n=%d %+v", len(recs), sum)
	}
}

func TestCollectorZeroAndUnsupported(t *testing.T) {
	c := tokenusage.NewCollector(tokenusage.NewRegistry())
	ex := &tokenusage.AgentExecution{ID: "e", TaskID: "JOB-Z", Runtime: tokenusage.RuntimeAntigravity}
	_ = c.Start(ex)
	u, err := c.CaptureFromSession(context.Background(), ex, nil)
	if err == nil || u.UsageStatus != tokenusage.StatusUnavailable || u.HasAny() {
		t.Fatalf("%+v %v", u, err)
	}
	sum, _ := c.Finalize("JOB-Z")
	if sum.TotalTokens != 0 || sum.UsageStatus != tokenusage.StatusUnavailable {
		t.Fatalf("%+v", sum)
	}
}

func TestCachedNotAddedIntoTotal(t *testing.T) {
	u := &tokenusage.TokenUsage{InputTokens: 10000, CachedInputTokens: 8000, OutputTokens: 500}
	u.Normalize()
	if u.TotalTokens != 10500 {
		t.Fatalf("cached 被重复计入: %d", u.TotalTokens)
	}
}
