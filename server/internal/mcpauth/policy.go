package mcpauth

import (
	"context"
	"time"
)

// ExternalPolicy is an explicit grant for the shared workflow/skill/knowledge
// library. No row means read-only; platform roles do not implicitly grant writes.
type ExternalPolicy struct {
	UserID            string    `json:"user_id"`
	CanWrite          bool      `json:"can_write"`
	CollectorCanWrite bool      `json:"collector_can_write"`
	UpdatedBy         string    `json:"updated_by,omitempty"`
	UpdatedAt         time.Time `json:"updated_at,omitempty"`
}

func (s *Service) ExternalPolicy(ctx context.Context, userID string) (*ExternalPolicy, error) {
	return s.store.GetExternalPolicy(ctx, userID)
}

func (s *Service) SetExternalPolicy(ctx context.Context, userID string, canWrite bool, actorID string) (*ExternalPolicy, error) {
	return s.SetExternalServicePolicy(ctx, userID, Workflow, canWrite, actorID)
}

func (p *ExternalPolicy) CanWriteService(service string) bool {
	if service == Collector {
		return p.CollectorCanWrite
	}
	return service == Workflow && p.CanWrite
}

func (s *Service) SetExternalServicePolicy(ctx context.Context, userID, service string, canWrite bool, actorID string) (*ExternalPolicy, error) {
	if service != Workflow && service != Collector {
		return nil, ErrInvalidToken
	}
	return s.store.SetExternalServicePolicy(ctx, userID, service, canWrite, actorID)
}

func (m *MemoryStore) SetExternalServicePolicy(_ context.Context, userID, service string, canWrite bool, actorID string) (*ExternalPolicy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := ExternalPolicy{UserID: userID}
	if previous := m.policies[userID]; previous != nil {
		p = *previous
	}
	if service == Collector {
		p.CollectorCanWrite = canWrite
	} else {
		p.CanWrite = canWrite
	}
	p.UpdatedBy, p.UpdatedAt = actorID, time.Now().UTC()
	m.policies[userID] = &p
	cp := p
	return &cp, nil
}

func (m *MemoryStore) GetExternalPolicy(_ context.Context, userID string) (*ExternalPolicy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if p, ok := m.policies[userID]; ok {
		cp := *p
		return &cp, nil
	}
	return &ExternalPolicy{UserID: userID}, nil
}

func (m *MemoryStore) SetExternalPolicy(_ context.Context, p *ExternalPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	m.policies[p.UserID] = &cp
	return nil
}
