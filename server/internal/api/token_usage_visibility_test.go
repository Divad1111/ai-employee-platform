package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/job"
)

func TestUsageBreakdownPreservesMonthlyCounterAfterJobDeletion(t *testing.T) {
	d := Deps{}
	r := httptest.NewRequest("GET", "/api/dashboard", nil)
	s := &auth.Session{UserID: "user"}
	for _, retained := range []int64{0, 1000, 37355, 40000} {
		var jobs []*job.Job
		if retained > 0 {
			jobs = []*job.Job{{CreatedBy: "user", CreatedAt: time.Now(), TotalTokens: retained, TokenUsageStatus: "FINAL"}}
		}
		out := d.attachUsageBreakdown(r, s, map[string]any{"tokens_used": int64(37355), "token_limit": int64(50000)}, jobs)
		if out["tokens_used"] != max(retained, int64(37355)) {
			t.Fatalf("retained=%d, usage=%v", retained, out)
		}
		if out["unattributed_tokens"] != max(int64(0), 37355-retained) {
			t.Fatalf("retained=%d, unattributed=%v", retained, out["unattributed_tokens"])
		}
	}
}
