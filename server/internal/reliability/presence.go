package reliability

import (
	"sync"
	"time"
)

// 在线状态常量。
const (
	StatusOnline  = "ONLINE"
	StatusOffline = "OFFLINE"
	StatusUnknown = "UNKNOWN"
)

// Presence 记录 Workstation 心跳与 Offline 判定。
// Offline 后 Scheduler（M6）不得分配新 Job；已有 RUNNING → UNKNOWN（不立即 FAILED）。
type Presence struct {
	mu              sync.Mutex
	byWS            map[string]*wsPresence
	heartbeatSec    int
	offlineAfterSec int
	nowFunc         func() time.Time
	// OnOffline 可选回调（测试/调度钩子）
	OnOffline func(wsID string)
}

type wsPresence struct {
	Status          string
	LastHeartbeat   time.Time
	Version         string
	Employees       uint32
	Sessions        uint32
	CPUPercent      float64
	MemoryPercent   float64
	DiskPercent     float64
}

// NewPresence 创建心跳跟踪器。默认约 5s 心跳、约 15s Offline。
func NewPresence(heartbeatSec, offlineAfterSec int) *Presence {
	if heartbeatSec <= 0 {
		heartbeatSec = 5
	}
	if offlineAfterSec <= 0 {
		offlineAfterSec = 15
	}
	return &Presence{
		byWS:            make(map[string]*wsPresence),
		heartbeatSec:    heartbeatSec,
		offlineAfterSec: offlineAfterSec,
		nowFunc:         time.Now,
	}
}

// HeartbeatInterval 建议客户端心跳间隔。
func (p *Presence) HeartbeatInterval() time.Duration {
	return time.Duration(p.heartbeatSec) * time.Second
}

// Touch 记录心跳并标记 ONLINE。
func (p *Presence) Touch(wsID, version string, employees, sessions uint32, cpu, mem, disk float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.byWS[wsID]
	if !ok {
		w = &wsPresence{}
		p.byWS[wsID] = w
	}
	w.Status = StatusOnline
	w.LastHeartbeat = p.nowFunc()
	w.Version = version
	w.Employees = employees
	w.Sessions = sessions
	w.CPUPercent = cpu
	w.MemoryPercent = mem
	w.DiskPercent = disk
}

// StatusOf 返回当前状态（不主动扫描超时）。
func (p *Presence) StatusOf(wsID string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.byWS[wsID]
	if !ok {
		return StatusUnknown
	}
	return w.Status
}

// IsSchedulable ONLINE 才可分配新 Job（M6 使用）。
func (p *Presence) IsSchedulable(wsID string) bool {
	return p.StatusOf(wsID) == StatusOnline
}

// SweepOffline 将超时节点标为 OFFLINE；返回新变 Offline 的 ID 列表。
func (p *Presence) SweepOffline() []string {
	p.mu.Lock()
	now := p.nowFunc()
	limit := time.Duration(p.offlineAfterSec) * time.Second
	var gone []string
	for id, w := range p.byWS {
		if w.Status != StatusOnline {
			continue
		}
		if now.Sub(w.LastHeartbeat) >= limit {
			w.Status = StatusOffline
			gone = append(gone, id)
		}
	}
	cb := p.OnOffline
	p.mu.Unlock()
	if cb != nil {
		for _, id := range gone {
			cb(id)
		}
	}
	return gone
}

// Snapshot 只读快照（测试与管理面）。
func (p *Presence) Snapshot(wsID string) (status string, last time.Time, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.byWS[wsID]
	if !ok {
		return "", time.Time{}, false
	}
	return w.Status, w.LastHeartbeat, true
}

// ResourceSnapshot 资源指标（调度精细化）。
func (p *Presence) ResourceSnapshot(wsID string) (status string, last time.Time, cpu, mem, disk float64, sessions uint32, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, ok := p.byWS[wsID]
	if !ok {
		return "", time.Time{}, 0, 0, 0, 0, false
	}
	return w.Status, w.LastHeartbeat, w.CPUPercent, w.MemoryPercent, w.DiskPercent, w.Sessions, true
}

// SetClock 测试注入时钟。
func (p *Presence) SetClock(fn func() time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nowFunc = fn
}

// MarkOnline 连接建立时标记（尚无心跳也可视为在线等待）。
func (p *Presence) MarkOnline(wsID string) {
	p.Touch(wsID, "", 0, 0, 0, 0, 0)
}

// Remove 节点被删除时立即移除心跳跟踪。
func (p *Presence) Remove(wsID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.byWS, wsID)
}

