// Package employee 管理 AI Employee 身份（≠ 进程）。
// 设计依据：设计文档 §2、§10、§105。
package employee

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/idgen"
)

// 状态。
const (
	StatusStopped  = "STOPPED"
	StatusActive   = "ACTIVE"
	StatusDisabled = "DISABLED"
)

// 错误。
var (
	ErrNotFound       = errors.New("employee 不存在")
	ErrInvalidInput   = errors.New("参数无效")
	ErrAlreadyExists  = errors.New("employee 已存在")
	ErrWSAccessDenied = errors.New("WORKSTATION_ACCESS_DENIED")
)

// Employee 领域对象。
type Employee struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	RoleSummary       string    `json:"role_summary"`
	DefaultProvider   string    `json:"default_provider"`
	DefaultModel      string    `json:"default_model"`
	WorkstationID     string    `json:"workstation_id"`
	WorkspaceID       string    `json:"workspace_id"`
	PermissionProfile string    `json:"permission_profile"`
	OwnerUserID       string    `json:"owner_user_id,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CreateInput 创建参数。
type CreateInput struct {
	Name              string `json:"name"`
	Description       string `json:"description"`
	RoleSummary       string `json:"role_summary"`
	DefaultProvider   string `json:"default_provider"`
	DefaultModel      string `json:"default_model"`
	WorkstationID     string `json:"workstation_id"`
	WorkspaceID       string `json:"workspace_id"`
	PermissionProfile string `json:"permission_profile"`
	OwnerUserID       string `json:"owner_user_id"`
}

// UpdateInput 更新参数（空字符串表示不改，除 Status）。
type UpdateInput struct {
	Name              *string
	Description       *string
	RoleSummary       *string
	DefaultProvider   *string
	DefaultModel      *string
	WorkstationID     *string
	WorkspaceID       *string
	PermissionProfile *string
	Status            *string
}

// Auditor 审计。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// Bus 事件发布。
type Bus interface {
	Publish(ctx context.Context, typ string, payload map[string]string) eventbus.Event
}

// Store 持久化。
type Store interface {
	Save(ctx context.Context, e *Employee) error
	Get(ctx context.Context, id string) (*Employee, error)
	List(ctx context.Context) ([]*Employee, error)
	Delete(ctx context.Context, id string) error
	FindByWorkspace(ctx context.Context, workspaceID string) (*Employee, error)
}

// Service Employee 服务。
type Service struct {
	store Store
	audit Auditor
	bus   Bus
}

// NewService 创建服务。
func NewService(store Store, audit Auditor, bus Bus) *Service {
	return &Service{store: store, audit: audit, bus: bus}
}

// Create 创建 Employee（可无 Active Session）。
func (s *Service) Create(ctx context.Context, in CreateInput, actorID, ip string) (*Employee, error) {
	if in.Name == "" {
		return nil, ErrInvalidInput
	}
	now := time.Now().UTC()
	e := &Employee{
		ID:                idgen.New("EMP"),
		Name:              in.Name,
		Description:       in.Description,
		RoleSummary:       in.RoleSummary,
		DefaultProvider:   in.DefaultProvider,
		DefaultModel:      in.DefaultModel,
		WorkstationID:     in.WorkstationID,
		WorkspaceID:       in.WorkspaceID,
		PermissionProfile: in.PermissionProfile,
		OwnerUserID:       in.OwnerUserID,
		Status:            StatusStopped,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := s.store.Save(ctx, e); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "employee.create", "success", ip, map[string]string{"id": e.ID, "name": e.Name})
	s.publish(ctx, e.ID, "create")
	return e, nil
}

// Update 修改。
func (s *Service) Update(ctx context.Context, id string, in UpdateInput, actorID, ip string) (*Employee, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil || e == nil {
		return nil, ErrNotFound
	}
	changes := []string{
		audit.FormatChange("名称", e.Name, derefOr(in.Name, e.Name)),
		audit.FormatChange("描述", e.Description, derefOr(in.Description, e.Description)),
		audit.FormatChange("角色摘要", e.RoleSummary, derefOr(in.RoleSummary, e.RoleSummary)),
		audit.FormatChange("驱动引擎", e.DefaultProvider, derefOr(in.DefaultProvider, e.DefaultProvider)),
		audit.FormatChange("模型", e.DefaultModel, derefOr(in.DefaultModel, e.DefaultModel)),
		audit.FormatChange("工作站", e.WorkstationID, derefOr(in.WorkstationID, e.WorkstationID)),
		audit.FormatChange("工作区", e.WorkspaceID, derefOr(in.WorkspaceID, e.WorkspaceID)),
		audit.FormatChange("权限配置", e.PermissionProfile, derefOr(in.PermissionProfile, e.PermissionProfile)),
		audit.FormatChange("状态", e.Status, derefOr(in.Status, e.Status)),
	}
	if in.Name != nil {
		e.Name = *in.Name
	}
	if in.Description != nil {
		e.Description = *in.Description
	}
	if in.RoleSummary != nil {
		e.RoleSummary = *in.RoleSummary
	}
	if in.DefaultProvider != nil {
		e.DefaultProvider = *in.DefaultProvider
	}
	if in.DefaultModel != nil {
		e.DefaultModel = *in.DefaultModel
	}
	if in.WorkstationID != nil {
		e.WorkstationID = *in.WorkstationID
	}
	if in.WorkspaceID != nil {
		e.WorkspaceID = *in.WorkspaceID
	}
	if in.PermissionProfile != nil {
		e.PermissionProfile = *in.PermissionProfile
	}
	if in.Status != nil {
		e.Status = *in.Status
	}
	e.UpdatedAt = time.Now().UTC()
	if err := s.store.Save(ctx, e); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "employee.update", "success", ip, map[string]string{
		"id": id, "name": e.Name, "summary": audit.JoinSummary(changes...),
	})
	s.publish(ctx, id, "update")
	return e, nil
}

// Disable 停用。
func (s *Service) Disable(ctx context.Context, id, actorID, ip string) (*Employee, error) {
	st := StatusDisabled
	return s.Update(ctx, id, UpdateInput{Status: &st}, actorID, ip)
}

// Delete 删除。
func (s *Service) Delete(ctx context.Context, id, actorID, ip string) error {
	if _, err := s.store.Get(ctx, id); err != nil {
		return ErrNotFound
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.audit.Log(ctx, "USER", actorID, "employee.delete", "success", ip, map[string]string{"id": id})
	s.publish(ctx, id, "delete")
	return nil
}

// Get / List
func (s *Service) Get(ctx context.Context, id string) (*Employee, error) {
	e, err := s.store.Get(ctx, id)
	if err != nil || e == nil {
		return nil, ErrNotFound
	}
	return e, nil
}

func (s *Service) List(ctx context.Context) ([]*Employee, error) {
	return s.store.List(ctx)
}

func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func (s *Service) publish(ctx context.Context, id, action string) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, "employee.changed", map[string]string{"id": id, "action": action})
}

// MemoryStore 内存实现。
type MemoryStore struct {
	mu   sync.RWMutex
	byID map[string]*Employee
}

// NewMemoryStore 创建。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*Employee{}}
}

func (m *MemoryStore) Save(_ context.Context, e *Employee) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *e
	m.byID[e.ID] = &cp
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Employee, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *e
	return &cp, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*Employee, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Employee, 0, len(m.byID))
	for _, e := range m.byID {
		cp := *e
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[id]; !ok {
		return ErrNotFound
	}
	delete(m.byID, id)
	return nil
}

func (m *MemoryStore) FindByWorkspace(_ context.Context, workspaceID string) (*Employee, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.byID {
		if e.WorkspaceID == workspaceID && e.Status != StatusDisabled {
			cp := *e
			return &cp, nil
		}
	}
	return nil, nil
}
