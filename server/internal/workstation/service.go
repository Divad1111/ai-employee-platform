// Package workstation 管理 Workstation 视图与吊销。
// 状态来自心跳 Presence；证书来自 CA。
// 设计依据：设计文档 §18、§105。
package workstation

import (
	"context"
	"errors"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/reliability"
)

// View 列表/详情视图。
type View struct {
	ID              string                   `json:"id"`
	Name            string                   `json:"name"`
	Status          string                   `json:"status"`
	Fingerprint     string                   `json:"fingerprint"`
	CertStatus      string                   `json:"cert_status"`
	LastHeartbeatAt time.Time                `json:"last_heartbeat_at,omitempty"`
	Version         string                   `json:"version,omitempty"`
	CPUPercent      float64                  `json:"cpu_percent"`
	MemoryPercent   float64                  `json:"memory_percent"`
	DiskPercent     float64                  `json:"disk_percent"`
	Providers       []string                 `json:"providers,omitempty"`
	Models          map[string][]ModelOption `json:"models,omitempty"`
}

// ModelOption 某个驱动引擎下可选的模型。
type ModelOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// MetaStore 可选的名称等元数据。
type MetaStore interface {
	Upsert(ctx context.Context, id, name string) error
	GetName(ctx context.Context, id string) string
	ListIDs(ctx context.Context) []string
	Delete(ctx context.Context, id string) error
}

// ModelCacheStore 持久化工作站上报的模型列表，避免服务重启后再次等待工作站同步。
type ModelCacheStore interface {
	SaveModelCache(context.Context, string, map[string][]ModelOption) error
	LoadModelCache(context.Context, string) (map[string][]ModelOption, time.Time, error)
	ModelCacheUpdatedAt(context.Context, string) (time.Time, error)
}

// WorkerCommander 接口支持向工作站下发指令并管理长连接生命周期。
type WorkerCommander interface {
	PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error)
	Disconnect(wsID string)
}

// Service Workstation 只读 + 吊销。
type Service struct {
	CA             *certca.Authority
	Presence       *reliability.Presence
	Meta           MetaStore
	Commander      WorkerCommander
	modelMu        sync.Mutex
	refreshPending map[string]bool
}

// NewService 创建。
func NewService(ca *certca.Authority, presence *reliability.Presence, meta MetaStore) *Service {
	if meta == nil {
		meta = NewMemoryMeta()
	}
	return &Service{CA: ca, Presence: presence, Meta: meta, refreshPending: make(map[string]bool)}
}

// SetCommander 绑定命令与连接管理器。
func (s *Service) SetCommander(c WorkerCommander) {
	s.Commander = c
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
		if rec.Status != "REVOKED" {
			ids[rec.WorkstationID] = struct{}{}
		}
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
	if cache, ok := s.Meta.(ModelCacheStore); ok {
		if models, _, err := cache.LoadModelCache(ctx, id); err == nil && len(models) > 0 {
			v.Models = models
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
		v.Providers = s.Presence.Providers(id)
		if reported := s.Presence.Models(id); len(reported) > 0 {
			v.Models = make(map[string][]ModelOption, len(reported))
			for name, list := range reported {
				for _, item := range list {
					v.Models[name] = append(v.Models[name], ModelOption{ID: item.ID, Label: item.Label})
				}
			}
		}
	}
	if v.CertStatus == "" && v.Status == reliability.StatusOnline {
		v.CertStatus = "ACTIVE"
	}
	return v
}

// ModelsUpdatedAt 返回最近一次模型上报时间。
func (s *Service) ModelsUpdatedAt(wsID string) time.Time {
	var updated time.Time
	if s.Presence != nil {
		updated = s.Presence.ModelsUpdatedAt(wsID)
	}
	if cache, ok := s.Meta.(ModelCacheStore); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if cachedAt, err := cache.ModelCacheUpdatedAt(ctx, wsID); err == nil && cachedAt.After(updated) {
			updated = cachedAt
		}
	}
	return updated
}

// CacheModels 保存工作站最新上报的模型清单。
func (s *Service) CacheModels(ctx context.Context, wsID string, models map[string][]ModelOption) error {
	if len(models) == 0 {
		return nil
	}
	cache, ok := s.Meta.(ModelCacheStore)
	if !ok {
		return nil
	}
	return cache.SaveModelCache(ctx, wsID, models)
}

// CacheModelsOnRefresh 仅在有用户主动刷新请求等待时落库；普通连接上报只更新 Presence。
func (s *Service) CacheModelsOnRefresh(ctx context.Context, wsID string, models map[string][]ModelOption) error {
	s.modelMu.Lock()
	if !s.refreshPending[wsID] {
		s.modelMu.Unlock()
		return nil
	}
	delete(s.refreshPending, wsID)
	s.modelMu.Unlock()
	return s.CacheModels(ctx, wsID, models)
}

// RequestModelRefresh 让在线工作站重新查询并上报模型列表。
func (s *Service) RequestModelRefresh(wsID string) error {
	if s.Commander == nil {
		return errors.New("工作站通道未就绪")
	}
	if s.Presence == nil || s.Presence.StatusOf(wsID) != reliability.StatusOnline {
		return errors.New("工作站离线，无法拉取模型列表")
	}
	s.modelMu.Lock()
	if s.refreshPending == nil {
		s.refreshPending = make(map[string]bool)
	}
	s.refreshPending[wsID] = true
	s.modelMu.Unlock()
	_, err := s.Commander.PushCommand(wsID, aiev1.CommandType_COMMAND_TYPE_UPDATE_WORKSTATION, "", "", `{"op":"refresh_models"}`)
	if err != nil {
		s.modelMu.Lock()
		delete(s.refreshPending, wsID)
		s.modelMu.Unlock()
	}
	return err
}

// Revoke 吊销证书。
func (s *Service) Revoke(fingerprint string) error {
	return s.CA.Revoke(fingerprint)
}

// Delete 删除工作站：
// 1. 若工作站当前在线，向 aew 下发 SHUTDOWN 命令通知其主动停机并清理本地身份；
// 2. 主动断开 gRPC 链路，避免其上报心跳复活；
// 3. 吊销并清理证书、移除心跳跟踪、从元数据及数据库中删除。
func (s *Service) Delete(ctx context.Context, id string) error {
	if s.Commander != nil {
		_, _ = s.Commander.PushCommand(id, aiev1.CommandType_COMMAND_TYPE_SHUTDOWN, "", "", "")
		time.Sleep(200 * time.Millisecond)
		s.Commander.Disconnect(id)
	}
	if s.Presence != nil {
		s.Presence.Remove(id)
	}
	if s.CA != nil {
		_ = s.CA.DeleteWorkstation(id)
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
	mu       sync.RWMutex
	names    map[string]string
	models   map[string]map[string][]ModelOption
	modelsAt map[string]time.Time
}

func NewMemoryMeta() *MemoryMeta {
	return &MemoryMeta{names: map[string]string{}, models: map[string]map[string][]ModelOption{}, modelsAt: map[string]time.Time{}}
}

func (m *MemoryMeta) SaveModelCache(_ context.Context, id string, models map[string][]ModelOption) error {
	if len(models) == 0 {
		return nil
	}
	copyModels := make(map[string][]ModelOption, len(models))
	for provider, list := range models {
		copyModels[provider] = append([]ModelOption(nil), list...)
	}
	m.mu.Lock()
	m.models[id] = copyModels
	m.modelsAt[id] = time.Now().UTC()
	m.mu.Unlock()
	return nil
}

func (m *MemoryMeta) LoadModelCache(_ context.Context, id string) (map[string][]ModelOption, time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	models := m.models[id]
	if len(models) == 0 {
		return nil, m.modelsAt[id], nil
	}
	copyModels := make(map[string][]ModelOption, len(models))
	for provider, list := range models {
		copyModels[provider] = append([]ModelOption(nil), list...)
	}
	return copyModels, m.modelsAt[id], nil
}

func (m *MemoryMeta) ModelCacheUpdatedAt(_ context.Context, id string) (time.Time, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.modelsAt[id], nil
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
	delete(m.models, id)
	delete(m.modelsAt, id)
	return nil
}
