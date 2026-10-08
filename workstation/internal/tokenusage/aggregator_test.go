package tokenusage_test

import (
	"testing"

	"github.com/ai-employee-platform/workstation/internal/tokenusage"
)

func TestAggregateMultiRun(t *testing.T) {
	sum := tokenusage.Aggregate([]*tokenusage.TokenUsage{
		{InputTokens: 100, OutputTokens: 10, TotalTokens: 110, UsageStatus: tokenusage.StatusFinal},
		{InputTokens: 200, OutputTokens: 20, CachedInputTokens: 50, UsageStatus: tokenusage.StatusFinal},
	})
	if sum.InputTokens != 300 || sum.OutputTokens != 30 || sum.CachedInputTokens != 50 {
		t.Fatalf("%+v", sum)
	}
	if sum.TotalTokens != 330 { // 200+20 缺 Total 时 Normalize 后累加
		// 第二条：第二条 Normalize 补 Total=220，合计 110+220=330
		t.Fatalf("total=%d", sum.TotalTokens)
	}
	if sum.UsageStatus != tokenusage.StatusFinal || sum.RunCount != 2 {
		t.Fatalf("%+v", sum)
	}
}

func TestAggregatePartial(t *testing.T) {
	sum := tokenusage.Aggregate([]*tokenusage.TokenUsage{
		{InputTokens: 1, OutputTokens: 1, UsageStatus: tokenusage.StatusFinal},
		{UsageStatus: tokenusage.StatusPending},
	})
	if sum.UsageStatus != tokenusage.StatusPartial {
		t.Fatalf("%+v", sum)
	}
}

func TestNoEstimateInNormalize(t *testing.T) {
	u := &tokenusage.TokenUsage{}
	u.Normalize()
	if u.UsageStatus != tokenusage.StatusUnavailable {
		t.Fatal(u.UsageStatus)
	}
}
