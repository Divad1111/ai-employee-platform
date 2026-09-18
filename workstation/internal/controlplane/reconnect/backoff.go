package reconnect

import (
	"math"
	"sync"
	"time"
)

// 连接状态。
const (
	StateConnected    = "CONNECTED"
	StateDisconnected = "DISCONNECTED"
	StateReconnecting = "RECONNECTING"
)

// Backoff 指数退避：1s/2s/4s/... 有上限。
type Backoff struct {
	mu          sync.Mutex
	state       string
	attempt     int
	base        time.Duration
	max         time.Duration
	onState     func(old, neo string)
}

// NewBackoff 创建退避器。max<=0 时默认 60s。
func NewBackoff(max time.Duration) *Backoff {
	if max <= 0 {
		max = 60 * time.Second
	}
	return &Backoff{
		state: StateDisconnected,
		base:  time.Second,
		max:   max,
	}
}

// OnStateChange 注册状态回调。
func (b *Backoff) OnStateChange(fn func(old, neo string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onState = fn
}

// State 当前状态。
func (b *Backoff) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

func (b *Backoff) setLocked(neo string) {
	old := b.state
	if old == neo {
		return
	}
	b.state = neo
	fn := b.onState
	if fn != nil {
		go fn(old, neo)
	}
}

// MarkConnected 连接成功，重置退避。
func (b *Backoff) MarkConnected() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempt = 0
	b.setLocked(StateConnected)
}

// MarkDisconnected 标记断开。
func (b *Backoff) MarkDisconnected() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == StateConnected || b.state == StateReconnecting {
		b.setLocked(StateDisconnected)
	}
}

// BeginReconnect 进入 RECONNECTING，返回本次应等待时长。
func (b *Backoff) BeginReconnect() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setLocked(StateReconnecting)
	wait := b.base * time.Duration(math.Pow(2, float64(b.attempt)))
	if wait > b.max {
		wait = b.max
	}
	if wait < b.base {
		wait = b.base
	}
	b.attempt++
	return wait
}

// NextDelay 只读下一退避（不改状态），用于测试。
func (b *Backoff) NextDelay() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	wait := b.base * time.Duration(math.Pow(2, float64(b.attempt)))
	if wait > b.max {
		wait = b.max
	}
	return wait
}
