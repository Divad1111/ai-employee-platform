//go:build darwin

package monitor_test

import (
	"testing"

	"github.com/ai-employee-platform/workstation/internal/monitor"
)

func TestDarwinHostMetrics(t *testing.T) {
	s := &monitor.Sampler{}
	sample := s.Sample()
	if sample.MemoryPercent <= 0 || sample.MemoryPercent > 100 {
		t.Fatalf("memory %+v", sample)
	}
	if sample.DiskPercent <= 0 || sample.DiskPercent > 100 {
		t.Fatalf("disk %+v", sample)
	}
	if sample.CPUPercent < 0 || sample.CPUPercent > 100 {
		t.Fatalf("cpu %+v", sample)
	}
}
