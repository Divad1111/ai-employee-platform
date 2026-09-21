// Package workstation 管理 Workstation 视图与吊销。
// 状态来自心跳 Presence；证书来自 CA。
// 设计依据：设计文档 §18、§105。
package workstation

import (
	"context"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/reliability"
)

// View 列表/详情视图。
type View struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	Fingerprint     string    `json:"fingerprint"`
	CertStatus      string    `json:"cert_status"`
	LastHeartbeatAt time.Time `json:"last_heartbeat_at,omitempty"`
	Version         string    `json:"version,omitempty"`
	CPUPercent      float64   `json:"cpu_percent,omitempty"`
	MemoryPercent   float64   `json:"memory_percent,omitempty"`
	DiskPercent     float64   `json:"disk_percent,omitempty"`
}

// MetaStore 可选的名称等元数据。
type MetaStore interface {
	Upsert(ctx context.Context, id, name string) error
	GetName(ctx context.Context, id string) string
	ListIDs(ctx context.Context) []string
	Delete(ctx context.Context, id string) error
}

// Service Workstation 只读 + 吊销。
type Service struct {
	CA       *certca.Authority
	Presence *reliability.Presence
	Meta     MetaStore
}

// NewService 创建。
func NewService(ca *certca.Authority, presence *reliability.Presence, meta MetaStore) *Service {
	if meta == nil {
		meta = NewMemoryMeta()
	}
	return &Service{CA: ca, Presence: presence, Meta: meta}
}

// EnsureRegistered Enrollment 后登记。
func (s *Service) EnsureRegistered(ctx context.Context, id, name string) {
	if name == "" {
		name = id
	}
	_ = s.Meta.Upsert(ctx, id, name)
}

// List 合并证书与心跳状态。
func (s *Service) List(ctx context.Context) []View {
	ids := map[string]struct{}{}
	for _, id := range s.Meta.ListIDs(ctx) {
		ids[id] = struct{}{}
	}
	for _, rec := range s.CA.ListRecords() {
		ids[rec.WorkstationID] = struct{}{}
	}
	out := make([]View, 0, len(ids))
	for id := range ids {
		out = append(out, s.Get(ctx, id))
	}
	return out
}

// Get 详情。
func (s *Service) Get(ctx context.Context, id string) View {
	v := View{ID: id, Name: s.Meta.GetName(ctx, id), Status: reliability.StatusUnknown}
	if v.Name == "" {
		v.Name = id
	}
	if rec := s.CA.FindByWorkstation(id); rec != nil {
		v.Fingerprint = rec.Fingerprint
		v.CertStatus = rec.Status
		if rec.Status == "REVOKED" {
			v.Status = "REVOKED"
			return v
		}
	}
	if s.Presence != nil {
		st, last, cpu, mem, disk, _, ok := s.Presence.ResourceSnapshot(id)
		if ok {
			v.Status = st
			v.LastHeartbeatAt = last
			v.CPUPercent = cpu
			v.MemoryPercent = mem
			v.DiskPercent = disk
		}
	}
	if v.CertStatus == "" && v.Status == reliability.StatusOnline {
		v.CertStatus = "ACTIVE"
	}
	return v
}

// Revoke 吊销证书。
func (s *Service) Revoke(fingerprint string) error {
	return s.CA.Revoke(fingerprint)
}

// Delete 删除工作站：吊销并清理证书、移除心跳跟踪、从元数据及数据库中删除。
func (s *Service) Delete(ctx context.Context, id string) error {
	if s.CA != nil {
		_ = s.CA.DeleteWorkstation(id)
	}
	if s.Presence != nil {
		s.Presence.Remove(id)
	}
	if s.Meta != nil {
		if err := s.Meta.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// MemoryMeta 元数据。
type MemoryMeta struct {
	mu    sync.RWMutex
	names map[string]string
}

func NewMemoryMeta() *MemoryMeta {
	return &MemoryMeta{names: map[string]string{}}
}

func (m *MemoryMeta) Upsert(_ context.Context, id, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.names[id] = name
	return nil
}

func (m *MemoryMeta) GetName(_ context.Context, id string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.names[id]
}

func (m *MemoryMeta) ListIDs(_ context.Context) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.names))
	for id := range m.names {
		out = append(out, id)
	}
	return out
}

func (m *MemoryMeta) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.names, id)
	return nil
}
