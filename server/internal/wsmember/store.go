// Package wsmember 管理 Workstation ↔ User 成员关系（§11.1）。
package wsmember

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

var (
	ErrNotMember   = errors.New("WORKSTATION_ACCESS_DENIED")
	ErrNotFound    = errors.New("成员关系不存在")
	ErrInvalidRole = errors.New("无效的站内角色")
)

const (
	RoleOwner  = "OWNER"
	RoleMember = "MEMBER"
	StatusActive = "active"
)

// Membership 成员行。
type Membership struct {
	ID            string    `json:"id"`
	WorkstationID string    `json:"workstation_id"`
	UserID        string    `json:"user_id"`
	Role          string    `json:"role"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Store 持久化。
type Store interface {
	Upsert(ctx context.Context, m *Membership) error
	Remove(ctx context.Context, workstationID, userID string) error
	Get(ctx context.Context, workstationID, userID string) (*Membership, error)
	ListByUser(ctx context.Context, userID string) ([]*Membership, error)
	ListByWorkstation(ctx context.Context, workstationID string) ([]*Membership, error)
	HasAccess(ctx context.Context, workstationID, userID string) (bool, error)
}

// MemoryStore 内存实现。
type MemoryStore struct {
	mu   sync.RWMutex
	byKey map[string]*Membership // wsID|userID
}

// NewMemoryStore 创建。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byKey: map[string]*Membership{}}
}

func key(ws, user string) string { return ws + "|" + user }

func (m *MemoryStore) Upsert(_ context.Context, mem *Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mem.Role != RoleOwner && mem.Role != RoleMember {
		return ErrInvalidRole
	}
	if mem.Status == "" {
		mem.Status = StatusActive
	}
	now := time.Now().UTC()
	k := key(mem.WorkstationID, mem.UserID)
	if old := m.byKey[k]; old != nil {
		mem.ID = old.ID
		mem.CreatedAt = old.CreatedAt
	} else if mem.ID == "" {
		mem.ID = idgen.New("WSM")
		mem.CreatedAt = now
	}
	mem.UpdatedAt = now
	cp := *mem
	m.byKey[k] = &cp
	return nil
}

func (m *MemoryStore) Remove(_ context.Context, workstationID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byKey, key(workstationID, userID))
	return nil
}

func (m *MemoryStore) Get(_ context.Context, workstationID, userID string) (*Membership, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v := m.byKey[key(workstationID, userID)]
	if v == nil || v.Status != StatusActive {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *MemoryStore) ListByUser(_ context.Context, userID string) ([]*Membership, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Membership
	for _, v := range m.byKey {
		if v.UserID == userID && v.Status == StatusActive {
			cp := *v
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) ListByWorkstation(_ context.Context, workstationID string) ([]*Membership, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Membership
	for _, v := range m.byKey {
		if v.WorkstationID == workstationID && v.Status == StatusActive {
			cp := *v
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) HasAccess(ctx context.Context, workstationID, userID string) (bool, error) {
	_, err := m.Get(ctx, workstationID, userID)
	if err == ErrNotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}
