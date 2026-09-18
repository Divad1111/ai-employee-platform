// Package session 管理 Agent Session 状态机。
// V1：每 Employee 最多 1 个 Active Session（决策 Q-03）。
// 设计依据：设计文档 §3、§16、§51、§52。
package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/idgen"
)

// 状态。
const (
	StatusStarting = "STARTING"
	StatusReady    = "READY"
	StatusBusy     = "BUSY"
	StatusStopping = "STOPPING"
	StatusStopped  = "STOPPED"
	StatusError    = "ERROR"
)

// Active 视为占用配额的状态。
var activeStatuses = map[string]bool{
	StatusStarting: true,
	StatusReady:    true,
	StatusBusy:     true,
	StatusStopping: true,
}

// 合法转换。
var transitions = map[string]map[string]bool{
	StatusStopped:  {StatusStarting: true},
	StatusStarting: {StatusReady: true, StatusError: true, StatusStopped: true},
	StatusReady:    {StatusBusy: true, StatusStopping: true, StatusError: true},
	StatusBusy:     {StatusReady: true, StatusStopping: true, StatusError: true},
	StatusStopping: {StatusStopped: true, StatusError: true},
	StatusError:    {StatusStopped: true, StatusStarting: true},
}

// 错误。
var (
	ErrNotFound         = errors.New("session 不存在")
	ErrInvalidTransition = errors.New("非法状态转换")
	ErrActiveLimit      = errors.New("该 Employee 已有 Active Session")
	ErrInvalidInput     = errors.New("参数无效")
)

// Session 领域对象。
type Session struct {
	ID             string    `json:"id"`
	EmployeeID     string    `json:"employee_id"`
	WorkstationID  string    `json:"workstation_id"`
	WorkspaceID    string    `json:"workspace_id"`
	Provider       string    `json:"provider"`
	ProcessID      int       `json:"process_id"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	LastActivityAt time.Time `json:"last_activity_at,omitempty"`
	EndedAt        time.Time `json:"ended_at,omitempty"`
}

// CreateInput 创建。
type CreateInput struct {
	EmployeeID    string `json:"employee_id"`
	WorkstationID string `json:"workstation_id"`
	WorkspaceID   string `json:"workspace_id"`
	Provider      string `json:"provider"`
}

// Auditor / Bus。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

type Bus interface {
	Publish(ctx context.Context, typ string, payload map[string]string) eventbus.Event
}

// Store 持久化。
type Store interface {
	Save(ctx context.Context, s *Session) error
	Get(ctx context.Context, id string) (*Session, error)
	List(ctx context.Context) ([]*Session, error)
	ListByEmployee(ctx context.Context, employeeID string) ([]*Session, error)
}

// Service Session 服务。
type Service struct {
	store Store
	audit Auditor
	bus   Bus
}

func NewService(store Store, audit Auditor, bus Bus) *Service {
	return &Service{store: store, audit: audit, bus: bus}
}

// Create 创建并进入 STARTING；强制每 Employee 至多 1 个 Active。
func (s *Service) Create(ctx context.Context, in CreateInput, actorID, ip string) (*Session, error) {
	if in.EmployeeID == "" {
		return nil, ErrInvalidInput
	}
	existing, err := s.store.ListByEmployee(ctx, in.EmployeeID)
	if err != nil {
		return nil, err
	}
	for _, e := range existing {
		if activeStatuses[e.Status] {
			return nil, ErrActiveLimit
		}
	}
	now := time.Now().UTC()
	sess := &Session{
		ID:             idgen.New("SES"),
		EmployeeID:     in.EmployeeID,
		WorkstationID:  in.WorkstationID,
		WorkspaceID:    in.WorkspaceID,
		Provider:       in.Provider,
		Status:         StatusStarting,
		CreatedAt:      now,
		StartedAt:      now,
		LastActivityAt: now,
	}
	if err := s.store.Save(ctx, sess); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "session.create", "success", ip, map[string]string{"id": sess.ID})
	s.publish(ctx, sess)
	return sess, nil
}

// Transition 状态转换。
func (s *Service) Transition(ctx context.Context, id, to, actorID, ip string) (*Session, error) {
	sess, err := s.store.Get(ctx, id)
	if err != nil || sess == nil {
		return nil, ErrNotFound
	}
	allowed := transitions[sess.Status]
	if allowed == nil || !allowed[to] {
		return nil, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, sess.Status, to)
	}
	sess.Status = to
	sess.LastActivityAt = time.Now().UTC()
	if to == StatusStopped || to == StatusError {
		sess.EndedAt = sess.LastActivityAt
	}
	if err := s.store.Save(ctx, sess); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "session.transition", "success", ip, map[string]string{"id": id, "to": to})
	s.publish(ctx, sess)
	return sess, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Session, error) {
	sess, err := s.store.Get(ctx, id)
	if err != nil || sess == nil {
		return nil, ErrNotFound
	}
	return sess, nil
}

func (s *Service) List(ctx context.Context) ([]*Session, error) {
	return s.store.List(ctx)
}

func (s *Service) publish(ctx context.Context, sess *Session) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, eventbus.TypeSessionStatus, map[string]string{
		"session_id":  sess.ID,
		"employee_id": sess.EmployeeID,
		"status":      sess.Status,
	})
}

// MemoryStore 内存。
type MemoryStore struct {
	mu   sync.RWMutex
	byID map[string]*Session
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*Session{}}
}

func (m *MemoryStore) Save(_ context.Context, s *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	m.byID[s.ID] = &cp
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.byID))
	for _, s := range m.byID {
		cp := *s
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) ListByEmployee(_ context.Context, employeeID string) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Session
	for _, s := range m.byID {
		if s.EmployeeID == employeeID {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}
