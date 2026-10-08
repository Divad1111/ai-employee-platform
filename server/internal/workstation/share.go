package workstation

import (
	"context"
	"strings"
	"sync"
)

// Share 工作站是否公用，以及公用时默认授权的平台角色。
type Share struct {
	WorkstationID string
	IsPublic      bool
	CreatedBy     string
	Roles         []string
}

// ShareStore 公用设置与角色授权。
type ShareStore interface {
	Get(ctx context.Context, workstationID string) (Share, error)
	Save(ctx context.Context, share Share) error
	ListPublic(ctx context.Context) ([]Share, error)
}

// MemoryShare 内存实现，供测试使用。
type MemoryShare struct {
	mu sync.RWMutex
	by map[string]Share
}

func NewMemoryShare() *MemoryShare {
	return &MemoryShare{by: map[string]Share{}}
}

func (m *MemoryShare) Get(_ context.Context, workstationID string) (Share, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := m.by[workstationID]
	s.WorkstationID = workstationID
	s.Roles = append([]string(nil), s.Roles...)
	return s, nil
}

func (m *MemoryShare) Save(_ context.Context, share Share) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !share.IsPublic {
		share.Roles = nil
	}
	share.Roles = cleanRoleNames(share.Roles)
	m.by[share.WorkstationID] = share
	return nil
}

func (m *MemoryShare) ListPublic(_ context.Context) ([]Share, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Share
	for _, s := range m.by {
		if !s.IsPublic {
			continue
		}
		s.Roles = append([]string(nil), s.Roles...)
		out = append(out, s)
	}
	return out, nil
}

func cleanRoleNames(roles []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		key := strings.ToUpper(role)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, role)
	}
	return out
}

// RoleGranted 用户角色是否命中授权列表。
func RoleGranted(userRoles, grantRoles []string) bool {
	for _, have := range userRoles {
		for _, want := range grantRoles {
			if strings.EqualFold(strings.TrimSpace(have), strings.TrimSpace(want)) && strings.TrimSpace(want) != "" {
				return true
			}
		}
	}
	return false
}
