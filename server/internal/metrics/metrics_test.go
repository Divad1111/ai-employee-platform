package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/metrics"
)

func TestHandlerExposesRequiredSeries(t *testing.T) {
	r := metrics.New()
	r.WorkstationOnline.Store(1)
	r.JobRunning.Store(2)
	r.JobSuccess.Store(3)
	r.JobFailed.Store(4)
	r.ActiveSessions.Store(5)
	r.ACPConnections.Store(6)
	r.ObserveJobDuration(1.5)
	rr := httptest.NewRecorder()
	r.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rr.Code != 200 {
		t.Fatal(rr.Code)
	}
	body := rr.Body.String()
	for _, s := range []string{
		"aie_workstation_online 1",
		"aie_job_running 2",
		"aie_job_success_total 3",
		"aie_job_failed_total 4",
		"aie_active_sessions 5",
		"aie_acp_connection 6",
		"aie_job_duration_seconds",
	} {
		if !strings.Contains(body, s) {
			t.Fatalf("缺少 %q\n%s", s, body)
		}
	}
}
