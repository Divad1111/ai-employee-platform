package monitor_test

import (
	"testing"

	"github.com/ai-employee-platform/workstation/internal/monitor"
)

func TestSamplerReturnsMetrics(t *testing.T) {
	s := &monitor.Sampler{
		DiskFn: func() float64 { return 42 },
		MemFn:  func() (uint64, uint64) { return 1, 4 },
	}
	sample := s.Sample()
	if sample.DiskPercent != 42 || sample.MemoryPercent != 25 {
		t.Fatalf("%+v", sample)
	}
	if sample.Goroutines <= 0 {
		t.Fatal("goroutines")
	}
}
