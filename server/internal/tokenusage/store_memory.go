package tokenusage

import (
	"context"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// MemoryStore 内存实现。
type MemoryStore struct {
	mu   sync.Mutex
	byID map[string]*RunUsage
	// provider|runID -> id
	byRun map[string]string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*RunUsage{}, byRun: map[string]string{}}
}

func runKey(provider, runID string) string {
	return provider + "|" + runID
}

func (m *MemoryStore) Upsert(_ context.Context, u *RunUsage) (*RunUsage, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if u.ProviderRunID != "" {
		if id, ok := m.byRun[runKey(u.Provider, u.ProviderRunID)]; ok {
			old := m.byID[id]
			cp := *u
			cp.ID = old.ID
			cp.CreatedAt = old.CreatedAt
			cp.UpdatedAt = now
			m.byID[id] = &cp
			return &cp, false, nil
		}
	}
	cp := *u
	if cp.ID == "" {
		cp.ID = idgen.New("TKU")
	}
	cp.CreatedAt = now
	cp.UpdatedAt = now
	m.byID[cp.ID] = &cp
	if cp.ProviderRunID != "" {
		m.byRun[runKey(cp.Provider, cp.ProviderRunID)] = cp.ID
	}
	return &cp, true, nil
}

func (m *MemoryStore) ListByJob(_ context.Context, jobID string) ([]*RunUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*RunUsage, 0)
	for _, u := range m.byID {
		if u.JobID == jobID {
			cp := *u
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) GetByProviderRun(_ context.Context, provider, runID string) (*RunUsage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id, ok := m.byRun[runKey(provider, runID)]
	if !ok {
		return nil, nil
	}
	cp := *m.byID[id]
	return &cp, nil
}
