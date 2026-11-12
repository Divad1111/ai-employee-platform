package main

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/quota"
	"github.com/ai-employee-platform/server/internal/tokenusage"
)

func TestRecordJobTokensCountsEachRunOnce(t *testing.T) {
	ctx := context.Background()
	jobs := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	j, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: "employee", Prompt: "test", IdempotencyKey: "test"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	usage := tokenusage.NewService(tokenusage.NewMemoryStore(), jobTokenBridge{jobs: jobs})
	store := quota.NewMemoryStore()
	first := map[string]string{"provider": "codex", "provider_run_id": "first", "input_tokens": "1200", "output_tokens": "80", "total_tokens": "1280", "usage_source": "codex_cli_event", "usage_status": "FINAL"}
	second := map[string]string{"provider": "codex", "provider_run_id": "retry", "input_tokens": "200", "output_tokens": "20", "total_tokens": "220", "usage_source": "codex_cli_event", "usage_status": "FINAL"}
	for _, payload := range []map[string]string{first, first, second, second} {
		recordJobTokens(ctx, jobs, usage, store, j.ID, "node", "employee", payload)
	}
	sum, _, err := usage.GetJobUsage(ctx, j.ID)
	if err != nil || sum.TotalTokens != 1500 {
		t.Fatal(sum, err)
	}
	monthly, err := store.GetUsage(ctx, quota.TypeUser, "user", quota.PeriodMonthly)
	if err != nil || monthly.TokensUsed != 1500 || monthly.RequestsUsed != 2 {
		t.Fatalf("retry/duplicate billing: %+v, %v", monthly, err)
	}
}
