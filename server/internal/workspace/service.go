// Package workspace 管理工作区绑定。
// V1 Workspace Lock：每 Employee 独占一个 Workspace（决策 Q-01）。
// 设计依据：设计文档 §50、§120。
package workspace

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/idgen"
)

// 错误。
var (
	ErrNotFound     = errors.New("workspace 不存在")
	ErrInvalidInput = errors.New("参数无效")
	ErrLocked       = errors.New("workspace 已被其他 Employee 占用")
)

// Workspace 工作区。
type Workspace struct {
	ID            string    `json:"id"`
	WorkstationID string    `json:"workstation_id"`
	EmployeeID    string    `json:"employee_id"`
	Path          string    `json:"path"`
	Repository    string    `json:"repository"`
	Branch        string    `json:"branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// CreateInput 创建。
type CreateInput struct {
	WorkstationID string `json:"workstation_id"`
	Path          string `json:"path"`
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	EmployeeID    string `json:"employee_id"`
}

// EmployeeBinder 校验独占绑定。
type EmployeeBinder interface {
	FindByWorkspace(ctx context.Context, workspaceID string) (employeeID string, err error)
	BindWorkspace(ctx context.Context, employeeID, workspaceID string) error
}

// Auditor 审计。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// Store 持久化。
type Store interface {
	Save(ctx context.Context, w *Workspace) error
	Get(ctx context.Context, id string) (*Workspace, error)
	List(ctx context.Context) ([]*Workspace, error)
	Delete(ctx context.Context, id string) error
}

// Service Workspace 服务。
type Service struct {
	store  Store
	audit  Auditor
	binder EmployeeBinder // 可选；绑定校验
}

// NewService 创建。
func NewService(store Store, audit Auditor) *Service {
	return &Service{store: store, audit: audit}
}

// SetBinder 注入 Employee 绑定校验。
func (s *Service) SetBinder(b EmployeeBinder) { s.binder = b }

// Create 创建 Workspace（须先指定工作站节点，再登记该节点上的本机路径）。
func (s *Service) Create(ctx context.Context, in CreateInput, actorID, ip string) (*Workspace, error) {
	if in.Path == "" || in.WorkstationID == "" {
		return nil, ErrInvalidInput
	}
	now := time.Now().UTC()
	w := &Workspace{
		ID:            idgen.New("WS"),
		WorkstationID: in.WorkstationID,
		EmployeeID:    in.EmployeeID,
		Path:          in.Path,
		Repository:    in.Repository,
		Branch:        in.Branch,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.store.Save(ctx, w); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "workspace.create", "success", ip, map[string]string{"id": w.ID})
	return w, nil
}

// BindEmployee 绑定 Employee（独占锁）。
func (s *Service) BindEmployee(ctx context.Context, workspaceID, employeeID, actorID, ip string) (*Workspace, error) {
	w, err := s.store.Get(ctx, workspaceID)
	if err != nil || w == nil {
		return nil, ErrNotFound
	}
	if s.binder != nil {
		owner, err := s.binder.FindByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		if owner != "" && owner != employeeID {
			return nil, ErrLocked
		}
		if err := s.binder.BindWorkspace(ctx, employeeID, workspaceID); err != nil {
			return nil, err
		}
	}
	w.EmployeeID = employeeID
	w.UpdatedAt = time.Now().UTC()
	if err := s.store.Save(ctx, w); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "workspace.bind", "success", ip, map[string]string{
		"workspace_id": workspaceID, "employee_id": employeeID,
	})
	return w, nil
}

// Get / List / Delete / Update
func (s *Service) Get(ctx context.Context, id string) (*Workspace, error) {
	w, err := s.store.Get(ctx, id)
	if err != nil || w == nil {
		return nil, ErrNotFound
	}
	return w, nil
}

func (s *Service) List(ctx context.Context) ([]*Workspace, error) {
	return s.store.List(ctx)
}

func (s *Service) Update(ctx context.Context, id, workstationID, path, repo, branch, actorID, ip string) (*Workspace, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	changes := []string{
		audit.FormatChange("工作站", w.WorkstationID, pick(workstationID, w.WorkstationID)),
		audit.FormatChange("路径", w.Path, pick(path, w.Path)),
		audit.FormatChange("仓库", w.Repository, pick(repo, w.Repository)),
		audit.FormatChange("分支", w.Branch, pick(branch, w.Branch)),
	}
	if workstationID != "" {
		w.WorkstationID = workstationID
	}
	if path != "" {
		w.Path = path
	}
	if repo != "" {
		w.Repository = repo
	}
	if branch != "" {
		w.Branch = branch
	}
	w.UpdatedAt = time.Now().UTC()
	if err := s.store.Save(ctx, w); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "workspace.update", "success", ip, map[string]string{
		"id": id, "summary": audit.JoinSummary(changes...),
	})
	return w, nil
}

func pick(next, cur string) string {
	if next == "" {
		return cur
	}
	return next
}

func (s *Service) Delete(ctx context.Context, id, actorID, ip string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}
	s.audit.Log(ctx, "USER", actorID, "workspace.delete", "success", ip, map[string]string{"id": id})
	return nil
}

// MemoryStore 内存。
type MemoryStore struct {
	mu   sync.RWMutex
	byID map[string]*Workspace
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byID: map[string]*Workspace{}}
}

func (m *MemoryStore) Save(_ context.Context, w *Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *w
	m.byID[w.ID] = &cp
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Workspace, 0, len(m.byID))
	for _, w := range m.byID {
		cp := *w
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
	return nil
}

// EmployeeBridge 将 employee.Service 适配为 binder。
type EmployeeBridge struct {
	GetByWorkspace func(ctx context.Context, wsID string) (empID string, err error)
	SetWorkspace   func(ctx context.Context, empID, wsID string) error
}

func (b EmployeeBridge) FindByWorkspace(ctx context.Context, workspaceID string) (string, error) {
	return b.GetByWorkspace(ctx, workspaceID)
}
func (b EmployeeBridge) BindWorkspace(ctx context.Context, employeeID, workspaceID string) error {
	return b.SetWorkspace(ctx, employeeID, workspaceID)
}
