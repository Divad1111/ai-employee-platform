// Package outbox 实现断网不丢事件的发送循环。
// 设计依据：设计文档 §23。
package outbox

import (
	"context"
	"fmt"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// Store Outbox 持久化。
type Store interface {
	// Enqueue 写入 PENDING（断网时调用）。
	Enqueue(ev *aiev1.Event) error
	// ListPending 按 sequence 升序返回待发。
	ListPending(limit int) ([]*aiev1.Event, error)
	// MarkSent 标记已发送。
	MarkSent(eventID string) error
}

// SendFunc 实际上报。
type SendFunc func(ctx context.Context, ev *aiev1.Event) error

// Dispatcher 发送循环。
type Dispatcher struct {
	Store    Store
	Send     SendFunc
	Interval time.Duration
	mu       sync.Mutex
	running  bool
}

// Enqueue 业务侧写入事件（先落库再尝试发）。
func (d *Dispatcher) Enqueue(ev *aiev1.Event) error {
	if d.Store == nil {
		return fmt.Errorf("无 Outbox Store")
	}
	return d.Store.Enqueue(ev)
}

// Run 阻塞直到 ctx 取消；网络恢复后自动发送并标记完成。
func (d *Dispatcher) Run(ctx context.Context) error {
	if d.Interval <= 0 {
		d.Interval = 500 * time.Millisecond
	}
	d.mu.Lock()
	d.running = true
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.running = false
		d.mu.Unlock()
	}()

	t := time.NewTicker(d.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			_ = d.flush(ctx)
		}
	}
}

func (d *Dispatcher) flush(ctx context.Context) error {
	if d.Store == nil || d.Send == nil {
		return nil
	}
	items, err := d.Store.ListPending(32)
	if err != nil {
		return err
	}
	for _, ev := range items {
		if err := d.Send(ctx, ev); err != nil {
			return err // 断网：保留 PENDING，下次重试
		}
		if err := d.Store.MarkSent(ev.GetEventId()); err != nil {
			return err
		}
	}
	return nil
}

// FlushOnce 测试用单次刷出。
func (d *Dispatcher) FlushOnce(ctx context.Context) error {
	return d.flush(ctx)
}

// MemoryStore 内存 Outbox。
type MemoryStore struct {
	mu      sync.Mutex
	pending []*entry
	sent    map[string]struct{}
}

type entry struct {
	ev     *aiev1.Event
	status string
}

// NewMemoryStore 创建内存 Outbox。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{sent: make(map[string]struct{})}
}

func (m *MemoryStore) Enqueue(ev *aiev1.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.pending {
		if e.ev.GetEventId() == ev.GetEventId() {
			return nil
		}
	}
	if _, ok := m.sent[ev.GetEventId()]; ok {
		return nil
	}
	m.pending = append(m.pending, &entry{ev: ev, status: "PENDING"})
	return nil
}

func (m *MemoryStore) ListPending(limit int) ([]*aiev1.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*aiev1.Event
	for _, e := range m.pending {
		if e.status != "PENDING" {
			continue
		}
		out = append(out, e.ev)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryStore) MarkSent(eventID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.pending {
		if e.ev.GetEventId() == eventID {
			e.status = "SENT"
			m.sent[eventID] = struct{}{}
			return nil
		}
	}
	return fmt.Errorf("未知 event_id")
}

// PendingCount 测试辅助。
func (m *MemoryStore) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, e := range m.pending {
		if e.status == "PENDING" {
			n++
		}
	}
	return n
}
