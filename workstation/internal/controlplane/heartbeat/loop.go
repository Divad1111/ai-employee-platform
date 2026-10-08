package heartbeat

import (
	"context"
	"encoding/json"
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
	// Providers 本机已安装的驱动引擎（cursor / codex 等）。
	Providers []string
	// Models 各引擎当前账号可用的模型。
	Models []ModelInfo
}

// ModelInfo 心跳上报的一条模型。
type ModelInfo struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Label    string `json:"label"`
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

// Build 组装一帧心跳。模型列表只在注册或按需刷新时放入 Stats.Models。
func Build(workstationID, version, messageID string, now time.Time, st Stats) *aiev1.Heartbeat {
	if messageID == "" {
		messageID = now.Format("hb-20060102150405.000")
	}
	hb := &aiev1.Heartbeat{
		Meta: &aiev1.EnvelopeMeta{
			MessageId:       messageID,
			TimestampUnixMs: now.UnixMilli(),
		},
		WorkstationId:   workstationID,
		TimestampUnixMs: now.UnixMilli(),
		Version:         version,
		Resources: &aiev1.ResourceUsage{
			CpuPercent:    st.CPU,
			MemoryPercent: st.Memory,
			DiskPercent:   st.Disk,
		},
		Employees: st.Employees,
		Sessions:  st.Sessions,
	}
	for _, name := range st.Providers {
		if name == "" {
			continue
		}
		hb.Capabilities = append(hb.Capabilities, &aiev1.Capability{Key: "provider", Value: name})
	}
	for _, m := range st.Models {
		if m.Provider == "" || m.ID == "" {
			continue
		}
		raw, err := json.Marshal(m)
		if err != nil {
			continue
		}
		hb.Capabilities = append(hb.Capabilities, &aiev1.Capability{Key: "model", Value: string(raw)})
	}
	return hb
}

func (l *Loop) tick(ctx context.Context) error {
	st := Stats{}
	if l.Stats != nil {
		st = l.Stats()
	}
	id := ""
	if l.newID != nil {
		id = l.newID()
	}
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	return l.Send(ctx, Build(l.WorkstationID, l.Version, id, now, st))
}
