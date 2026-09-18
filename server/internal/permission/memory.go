package permission

import (
	"context"
	"sync"
)

// MemoryStore 内存 Profile/Rule 存储。
type MemoryStore struct {
	mu       sync.RWMutex
	profiles map[string]*Profile
	rules    map[string]map[string]*Rule // profile → action → rule
	defID    string
}

// NewMemoryStore 创建。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		profiles: map[string]*Profile{},
		rules:    map[string]map[string]*Rule{},
		defID:    "default",
	}
}

func (m *MemoryStore) SaveProfile(_ context.Context, p *Profile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.profiles[p.ID] = &cp
	if p.IsDefault {
		m.defID = p.ID
	}
	return nil
}

func (m *MemoryStore) GetProfile(_ context.Context, id string) (*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.profiles[id]
	if !ok {
		return nil, ErrProfileNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *MemoryStore) DefaultProfileID(_ context.Context) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defID
}

func (m *MemoryStore) ListProfiles(_ context.Context) ([]*Profile, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Profile, 0, len(m.profiles))
	for _, p := range m.profiles {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) UpsertRule(_ context.Context, r *Rule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.rules[r.ProfileID] == nil {
		m.rules[r.ProfileID] = map[string]*Rule{}
	}
	cp := *r
	m.rules[r.ProfileID][r.Action] = &cp
	return nil
}

func (m *MemoryStore) ListRules(_ context.Context, profileID string) ([]*Rule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rm := m.rules[profileID]
	out := make([]*Rule, 0, len(rm))
	for _, r := range rm {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) GetRule(_ context.Context, profileID, action string) (*Rule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rm := m.rules[profileID]
	if rm == nil {
		return nil, nil
	}
	r, ok := rm[action]
	if !ok {
		return nil, nil
	}
	cp := *r
	return &cp, nil
}
