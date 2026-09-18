// Package monitor 采集本机资源指标，经 Heartbeat 上报（不高频写库）。
// 设计依据：设计文档 §115、§67。
package monitor

import (
	"runtime"
)

// Sample 资源采样。
type Sample struct {
	CPUPercent    float64
	MemoryPercent float64
	DiskPercent   float64
	Goroutines    int
}

// Sampler 采样器。
type Sampler struct {
	// DiskFn 可注入磁盘采样
	DiskFn func() float64
	MemFn  func() (used, total uint64)
}

// Sample 采集一次。
func (s *Sampler) Sample() Sample {
	out := Sample{Goroutines: runtime.NumGoroutine()}
	if s.MemFn != nil {
		used, total := s.MemFn()
		if total > 0 {
			out.MemoryPercent = float64(used) / float64(total) * 100
		}
	} else {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		// 粗略：HeapInuse / 假设 8GiB，仅作本地展示
		out.MemoryPercent = float64(ms.HeapInuse) / (8 * 1024 * 1024 * 1024) * 100
	}
	if s.DiskFn != nil {
		out.DiskPercent = s.DiskFn()
	}
	// CPU：V1 用 goroutine 密度作占位，避免每秒写库
	out.CPUPercent = float64(out.Goroutines) * 0.1
	if out.CPUPercent > 100 {
		out.CPUPercent = 100
	}
	return out
}
