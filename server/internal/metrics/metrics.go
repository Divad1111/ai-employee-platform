// Package metrics Prometheus 文本格式基础指标。
// Tracing（gRPC/Job span）V2 延后，见 docs/TEST_MATRIX_V2.md。
// 设计依据：设计文档 §87。
package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry 进程内计数器/仪表。
type Registry struct {
	mu sync.Mutex

	WorkstationOnline atomic.Int64
	JobRunning        atomic.Int64
	JobSuccess        atomic.Int64
	JobFailed         atomic.Int64
	ActiveSessions    atomic.Int64
	ACPConnections    atomic.Int64

	jobDurations []float64 // 秒
}

// New 创建。
func New() *Registry { return &Registry{} }

// ObserveJobDuration 记录 Job 耗时。
func (r *Registry) ObserveJobDuration(sec float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobDurations = append(r.jobDurations, sec)
	if len(r.jobDurations) > 1000 {
		r.jobDurations = r.jobDurations[len(r.jobDurations)-1000:]
	}
}

// Handler Prometheus 文本。
func (r *Registry) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		write := func(name, help, typ string, val int64) {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", name, help, name, typ, name, val)
		}
		write("aie_workstation_online", "ONLINE workstations", "gauge", r.WorkstationOnline.Load())
		write("aie_job_running", "Running jobs", "gauge", r.JobRunning.Load())
		write("aie_job_success_total", "Successful jobs", "counter", r.JobSuccess.Load())
		write("aie_job_failed_total", "Failed jobs", "counter", r.JobFailed.Load())
		write("aie_active_sessions", "Active sessions", "gauge", r.ActiveSessions.Load())
		write("aie_acp_connection", "ACP connections", "gauge", r.ACPConnections.Load())
		r.mu.Lock()
		var sum float64
		for _, d := range r.jobDurations {
			sum += d
		}
		n := len(r.jobDurations)
		r.mu.Unlock()
		avg := 0.0
		if n > 0 {
			avg = sum / float64(n)
		}
		fmt.Fprintf(&b, "# HELP aie_job_duration_seconds Job duration average\n# TYPE aie_job_duration_seconds gauge\naie_job_duration_seconds %g\n", avg)
		fmt.Fprintf(&b, "# HELP aie_scrape_timestamp_seconds Scrape time\n# TYPE aie_scrape_timestamp_seconds gauge\naie_scrape_timestamp_seconds %d\n", time.Now().Unix())
		_, _ = w.Write([]byte(b.String()))
	}
}
