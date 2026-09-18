// Package message 实现 Message / Conversation 服务。
// 支持 Employee→Employee，不限于 User→Employee。
// 设计依据：设计文档 §14、§13、§111。
package message

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// sender / receiver 类型。
const (
	TypeUser     = "USER"
	TypeEmployee = "EMPLOYEE"
	TypeGroup    = "GROUP"
	TypeSystem   = "SYSTEM"
)

var (
	ErrInvalidInput = errors.New("参数无效")
	ErrNotFound     = errors.New("消息不存在")
)

var validParty = map[string]bool{
	TypeUser: true, TypeEmployee: true, TypeGroup: true, TypeSystem: true,
}

// Message 消息。
type Message struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	SenderType     string    `json:"sender_type"`
	SenderID       string    `json:"sender_id"`
	ReceiverType   string    `json:"receiver_type"`
	ReceiverID     string    `json:"receiver_id"`
	Content        string    `json:"content"`
	ReplyTo        string    `json:"reply_to,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// SendInput 发送参数。
type SendInput struct {
	ConversationID string `json:"conversation_id"`
	SenderType     string `json:"sender_type"`
	SenderID       string `json:"sender_id"`
	ReceiverType   string `json:"receiver_type"`
	ReceiverID     string `json:"receiver_id"`
	Content        string `json:"content"`
	ReplyTo        string `json:"reply_to"`
}

// Auditor 审计。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// Store 持久化。
type Store interface {
	Save(ctx context.Context, m *Message) error
	Get(ctx context.Context, id string) (*Message, error)
	List(ctx context.Context, limit int) ([]*Message, error)
	ListByParty(ctx context.Context, partyType, partyID string, limit int) ([]*Message, error)
}

// Service Message 服务。
type Service struct {
	store Store
	audit Auditor
}

func NewService(store Store, audit Auditor) *Service {
	return &Service{store: store, audit: audit}
}

// Send 发送消息。
func (s *Service) Send(ctx context.Context, in SendInput, actorID, ip string) (*Message, error) {
	if !validParty[in.SenderType] || !validParty[in.ReceiverType] {
		return nil, ErrInvalidInput
	}
	if in.SenderID == "" || in.ReceiverID == "" {
		return nil, ErrInvalidInput
	}
	msg := &Message{
		ID:             idgen.New("MSG"),
		ConversationID: in.ConversationID,
		SenderType:     in.SenderType,
		SenderID:       in.SenderID,
		ReceiverType:   in.ReceiverType,
		ReceiverID:     in.ReceiverID,
		Content:        in.Content,
		ReplyTo:        in.ReplyTo,
		CreatedAt:      time.Now().UTC(),
	}
	if err := s.store.Save(ctx, msg); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "message.send", "success", ip, map[string]string{
		"id": msg.ID, "from": in.SenderType + ":" + in.SenderID, "to": in.ReceiverType + ":" + in.ReceiverID,
	})
	return msg, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Message, error) {
	m, err := s.store.Get(ctx, id)
	if err != nil || m == nil {
		return nil, ErrNotFound
	}
	return m, nil
}

func (s *Service) List(ctx context.Context, limit int) ([]*Message, error) {
	return s.store.List(ctx, limit)
}

// MemoryStore 内存。
type MemoryStore struct {
	mu   sync.RWMutex
	byID map[string]*Message
	all  []*Message
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*Message{}}
}

func (m *MemoryStore) Save(_ context.Context, msg *Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *msg
	m.byID[msg.ID] = &cp
	m.all = append(m.all, &cp)
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	msg, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *msg
	return &cp, nil
}

func (m *MemoryStore) List(_ context.Context, limit int) ([]*Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.all) {
		limit = len(m.all)
	}
	out := make([]*Message, limit)
	for i := 0; i < limit; i++ {
		cp := *m.all[len(m.all)-1-i]
		out[i] = &cp
	}
	return out, nil
}

func (m *MemoryStore) ListByParty(_ context.Context, partyType, partyID string, limit int) ([]*Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Message
	for i := len(m.all) - 1; i >= 0; i-- {
		msg := m.all[i]
		if (msg.SenderType == partyType && msg.SenderID == partyID) ||
			(msg.ReceiverType == partyType && msg.ReceiverID == partyID) {
			cp := *msg
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}
