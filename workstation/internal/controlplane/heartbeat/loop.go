package heartbeat

import (
	"context"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// Sender 发送心跳帧。
type Sender func(ctx context.Context, hb *aiev1.Heartbeat) error

// Stats 采集资源占用（M7 可替换为真实采集）。
type Stats struct {
	Employees uint32
	Sessions  uint32
	CPU       float64
	Memory    float64
	Disk      float64
}

// StatsFunc 动态采样。
type StatsFunc func() Stats

// Loop 周期性发送 Heartbeat。
type Loop struct {
	WorkstationID string
	Version       string
	Interval      time.Duration
	Send          Sender
	Stats         StatsFunc
	newID         func() string
	now           func() time.Time
}

// Run 阻塞直到 ctx 取消。默认间隔 5s。
func (l *Loop) Run(ctx context.Context) error {
	if l.Interval <= 0 {
		l.Interval = 5 * time.Second
	}
	if l.newID == nil {
		l.newID = func() string {
			return time.Now().Format("hb-20060102150405.000")
		}
	}
	if l.now == nil {
		l.now = time.Now
	}
	t := time.NewTicker(l.Interval)
	defer t.Stop()
	// 立即发一帧
	if err := l.tick(ctx); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := l.tick(ctx); err != nil {
				return err
			}
		}
	}
}

func (l *Loop) tick(ctx context.Context) error {
	st := Stats{}
	if l.Stats != nil {
		st = l.Stats()
	}
	hb := &aiev1.Heartbeat{
		Meta: &aiev1.EnvelopeMeta{
			MessageId:       l.newID(),
			TimestampUnixMs: l.now().UnixMilli(),
		},
		WorkstationId:   l.WorkstationID,
		TimestampUnixMs: l.now().UnixMilli(),
		Version:         l.Version,
		Resources: &aiev1.ResourceUsage{
			CpuPercent:    st.CPU,
			MemoryPercent: st.Memory,
			DiskPercent:   st.Disk,
		},
		Employees: st.Employees,
		Sessions:  st.Sessions,
	}
	return l.Send(ctx, hb)
}
