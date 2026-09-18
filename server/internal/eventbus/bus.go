// Package eventbus 进程内事件总线（V1 无 NATS/Kafka/Redis）。
// 关键事件可持久化到内存/DB，供 Admin SSE 订阅。
// 设计依据：设计文档 §85、§77。
package eventbus

import (
	"context"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// 事件类型常量。
const (
	TypeWorkstationOnline  = "workstation.online"
	TypeWorkstationOffline = "workstation.offline"
	TypeJobProgress        = "job.progress"
	TypeJobStatus          = "job.status"
	TypeSessionStatus      = "session.status"
	TypeSystemAlert        = "system.alert"
	TypeEmployeeChanged    = "employee.changed"
)

// Event 内部事件。
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Payload   map[string]string `json:"payload"`
	CreatedAt time.Time         `json:"created_at"`
}

// Bus 进程内发布/订阅 + 环形缓冲。
type Bus struct {
	mu       sync.RWMutex
	subs     map[chan Event]struct{}
	history  []Event
	maxHist  int
}

// New 创建事件总线。
func New(maxHistory int) *Bus {
	if maxHistory <= 0 {
		maxHistory = 500
	}
	return &Bus{subs: map[chan Event]struct{}{}, maxHist: maxHistory}
}

// Publish 发布并持久化到历史。
func (b *Bus) Publish(_ context.Context, typ string, payload map[string]string) Event {
	ev := Event{
		ID:        idgen.Raw(),
		Type:      typ,
		Payload:   copyMap(payload),
		CreatedAt: time.Now().UTC(),
	}
	b.mu.Lock()
	b.history = append(b.history, ev)
	if len(b.history) > b.maxHist {
		b.history = b.history[len(b.history)-b.maxHist:]
	}
	subs := make([]chan Event, 0, len(b.subs))
	for ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default: // 慢消费者丢弃，避免阻塞
		}
	}
	return ev
}

// Subscribe 订阅；调用方必须 Unsubscribe。
func (b *Bus) Subscribe(buffer int) chan Event {
	if buffer <= 0 {
		buffer = 16
	}
	ch := make(chan Event, buffer)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe 取消订阅并关闭 channel。
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
		close(ch)
	}
	b.mu.Unlock()
}

// Recent 返回最近 n 条（新→旧）。
func (b *Bus) Recent(n int) []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n <= 0 || n > len(b.history) {
		n = len(b.history)
	}
	out := make([]Event, n)
	for i := 0; i < n; i++ {
		out[i] = b.history[len(b.history)-1-i]
	}
	return out
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
