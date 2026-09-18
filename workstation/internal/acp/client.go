// Package acp 实现 ACP 客户端抽象与 Fake 实现。
// 链路：SessionManager → Provider → ACP → Agent。
// 设计依据：设计文档 §35、§117、§118。
package acp

import (
	"context"
	"errors"
	"sync"
)

// 错误。
var (
	ErrNotStarted = errors.New("ACP 会话未启动")
	ErrHandshake  = errors.New("ACP 握手失败")
)

// Event Agent 推送事件。
type Event struct {
	Type    string
	Payload []byte
}

// Session ACP 会话。
type Session interface {
	Start(ctx context.Context) error
	Send(ctx context.Context, input []byte) error
	Events() <-chan Event
	Stop(ctx context.Context) error
	Ready() bool
}

// Client 创建会话。
type Client interface {
	Open(sessionID string) Session
}

// FakeClient 内存 Fake ACP（单元测试）。
type FakeClient struct {
	FailHandshake bool
}

func (c *FakeClient) Open(sessionID string) Session {
	return &FakeSession{id: sessionID, failHS: c.FailHandshake, events: make(chan Event, 8)}
}

// FakeSession Fake 会话。
type FakeSession struct {
	id      string
	failHS  bool
	mu      sync.Mutex
	ready   bool
	stopped bool
	events  chan Event
	sent    [][]byte
}

func (s *FakeSession) Start(ctx context.Context) error {
	if s.failHS {
		return ErrHandshake
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ready = true
	select {
	case s.events <- Event{Type: "ready", Payload: []byte(s.id)}:
	default:
	}
	return nil
}

func (s *FakeSession) Send(_ context.Context, input []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready || s.stopped {
		return ErrNotStarted
	}
	cp := append([]byte{}, input...)
	s.sent = append(s.sent, cp)
	select {
	case s.events <- Event{Type: "ack", Payload: cp}:
	default:
	}
	return nil
}

func (s *FakeSession) Events() <-chan Event { return s.events }

func (s *FakeSession) Stop(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
	s.ready = false
	return nil
}

func (s *FakeSession) Ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready && !s.stopped
}

// Sent 测试辅助。
func (s *FakeSession) Sent() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte{}, s.sent...)
}
