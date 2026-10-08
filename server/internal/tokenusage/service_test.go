package tokenusage_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/tokenusage"
)

type memJobs struct {
	last tokenusage.Summary
}

func (m *memJobs) ApplyTokenSummary(_ context.Context, _ string, sum tokenusage.Summary, _, _ string) (string, error) {
	m.last = sum
	return "u1", nil
}

func TestUpsertIdempotentAndAggregate(t *testing.T) {
	jobs := &memJobs{}
	svc := tokenusage.NewService(tokenusage.NewMemoryStore(), jobs)
	ctx := context.Background()

	_, sum, created, err := svc.Upsert(ctx, tokenusage.UpsertInput{
		JobID: "JOB-1", Provider: "codex", ProviderRunID: "run-1",
		InputTokens: 100, OutputTokens: 20, UsageStatus: tokenusage.StatusFinal, UsageSource: "codex_cli_event",
	})
	if err != nil || !created {
		t.Fatalf("create: %v created=%v", err, created)
	}
	if sum.TotalTokens != 120 || sum.UsageStatus != tokenusage.StatusFinal {
		t.Fatalf("sum=%+v", sum)
	}

	_, sum, created, err = svc.Upsert(ctx, tokenusage.UpsertInput{
		JobID: "JOB-1", Provider: "codex", ProviderRunID: "run-1",
		InputTokens: 100, OutputTokens: 20, UsageStatus: tokenusage.StatusFinal, UsageSource: "codex_cli_event",
	})
	if err != nil || created {
		t.Fatalf("idempotent: %v created=%v", err, created)
	}

	_, sum, _, err = svc.Upsert(ctx, tokenusage.UpsertInput{
		JobID: "JOB-1", Provider: "cursor", ProviderRunID: "run-2",
		InputTokens: 50, OutputTokens: 10, UsageStatus: tokenusage.StatusFinal, UsageSource: "cursor_acp_event",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sum.InputTokens != 150 || sum.OutputTokens != 30 || sum.RunCount != 2 {
		t.Fatalf("aggregate=%+v", sum)
	}
	if jobs.last.TotalTokens != 180 {
		t.Fatalf("job cache=%+v", jobs.last)
	}
}

func TestEstimatePayloadBecomesUnavailable(t *testing.T) {
	svc := tokenusage.NewService(tokenusage.NewMemoryStore(), &memJobs{})
	_, first, err := svc.UpsertFromEventPayload(context.Background(), "JOB-E", "WS-1", "EMP-1", map[string]string{
		"input_tokens": "12", "output_tokens": "3", "token_source": "estimate", "session_id": "ses-1", "agent": "cursor",
	})
	if err != nil || !first {
		t.Fatalf("err=%v first=%v", err, first)
	}
	sum, runs, err := svc.GetJobUsage(context.Background(), "JOB-E")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].UsageSource != "unavailable" {
		t.Fatalf("runs=%+v", runs)
	}
	if sum.UsageStatus != tokenusage.StatusUnavailable || sum.InputTokens != 0 || sum.OutputTokens != 0 {
		t.Fatalf("sum=%+v", sum)
	}
}
