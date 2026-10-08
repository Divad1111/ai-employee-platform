package api_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/tokenusage"
)

type apiJobTokenBridge struct {
	jobs *job.Service
}

func (b apiJobTokenBridge) ApplyTokenSummary(ctx context.Context, jobID string, sum tokenusage.Summary, agent, source string) (string, error) {
	return b.jobs.ApplyTokenSummary(ctx, jobID, job.TokenSummary{
		InputTokens:           sum.InputTokens,
		OutputTokens:          sum.OutputTokens,
		CachedInputTokens:     sum.CachedInputTokens,
		CacheWriteInputTokens: sum.CacheWriteInputTokens,
		CacheReadInputTokens:  sum.CacheReadInputTokens,
		ReasoningOutputTokens: sum.ReasoningOutputTokens,
		TotalTokens:           sum.TotalTokens,
		UsageStatus:           sum.UsageStatus,
	}, agent, source)
}

func TestJobTokenUsageHTTPIdempotentAndRetry(t *testing.T) {
	h, tok := setupAPI(t)
	code, emp := doJSON(t, h, http.MethodPost, "/api/employees", tok, map[string]string{"name": "Tok"})
	if code != 201 {
		t.Fatalf("employee %d %v", code, emp)
	}
	empID := emp["id"].(string)
	code, jobResp := doJSON(t, h, http.MethodPost, "/api/jobs", tok, map[string]any{
		"employee_id": empID, "prompt": "usage", "idempotency_key": "idem-tok-1",
	})
	if code != 201 {
		t.Fatalf("job %d %v", code, jobResp)
	}
	jobObj := jobResp["job"].(map[string]any)
	jobID := jobObj["id"].(string)

	body := map[string]any{
		"provider": "codex", "provider_run_id": "run-1", "provider_session_id": "ses-1",
		"input_tokens": 1000, "cached_input_tokens": 800, "output_tokens": 100,
		"reasoning_output_tokens": 20, "usage_status": "FINAL", "usage_source": "codex_cli_event",
		"digital_employee_id": empID,
	}
	code, first := doJSON(t, h, http.MethodPost, "/api/jobs/"+jobID+"/token-usage", tok, body)
	if code != http.StatusCreated {
		t.Fatalf("first %d %v", code, first)
	}
	code, again := doJSON(t, h, http.MethodPost, "/api/jobs/"+jobID+"/token-usage", tok, body)
	if code != http.StatusOK {
		t.Fatalf("idempotent %d %v", code, again)
	}
	if again["created"] == true {
		t.Fatalf("重复上传不应新建: %v", again)
	}

	code, retry := doJSON(t, h, http.MethodPost, "/api/jobs/"+jobID+"/token-usage", tok, map[string]any{
		"provider": "codex", "provider_run_id": "run-2",
		"input_tokens": 400, "output_tokens": 40, "usage_status": "FINAL", "usage_source": "codex_cli_event",
	})
	if code != http.StatusCreated {
		t.Fatalf("retry run %d %v", code, retry)
	}

	code, got := doJSON(t, h, http.MethodGet, "/api/jobs/"+jobID+"/token-usage", tok, nil)
	if code != 200 {
		t.Fatalf("get %d %v", code, got)
	}
	sum := got["summary"].(map[string]any)
	if int(sum["input_tokens"].(float64)) != 1400 || int(sum["output_tokens"].(float64)) != 140 {
		t.Fatalf("summary=%v", sum)
	}
	if int(sum["run_count"].(float64)) != 2 {
		t.Fatalf("runs lost: %v", got["runs"])
	}
	if sum["usage_status"] != "FINAL" {
		t.Fatalf("status=%v", sum["usage_status"])
	}

	errCode, jb := doJSON(t, h, http.MethodGet, "/api/jobs/"+jobID, tok, nil)
	if errCode != 200 {
		t.Fatal(jb)
	}
	if int(jb["total_tokens"].(float64)) != 1540 {
		t.Fatalf("job cache total=%v", jb["total_tokens"])
	}
}
