// Package workspace 管理工作区绑定。
// 用户工作区可绑定多个同一所有者的员工。旧绝对路径工作区保留独占绑定。
// 设计依据：设计文档 §50、§120。
package workspace

import (
	"context"
	"errors"
	"github.com/ai-employee-platform/gen/workspacedir"
	"strings"
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
	RootPath      string    `json:"root_path,omitempty"`
	OwnerUserID   string    `json:"owner_user_id,omitempty"`
	UserDirectory string    `json:"user_directory,omitempty"`
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
	RootPath      string `json:"root_path"`
	OwnerUserID   string `json:"-"`
	UserDirectory string `json:"-"`
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
	UserDirectory(context.Context, string, string) (string, error)
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
	if in.RootPath != "" {
		if !workspacedir.ValidName(in.Path) || in.OwnerUserID == "" {
			return nil, ErrInvalidInput
		}
		var err error
		in.UserDirectory, err = s.store.UserDirectory(ctx, in.OwnerUserID, in.UserDirectory)
		if err != nil {
			return nil, err
		}
		if !workspacedir.ValidName(in.UserDirectory) {
			return nil, ErrInvalidInput
		}
	}
	now := time.Now().UTC()
	w := &Workspace{
		ID:            idgen.New("WS"),
		RootPath:      in.RootPath,
		OwnerUserID:   in.OwnerUserID,
		UserDirectory: in.UserDirectory,
		WorkstationID: in.WorkstationID,
		EmployeeID:    in.EmployeeID,
		Path:          in.Path,
		Repository:    in.Repository,
		Branch:        in.Branch,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if in.RootPath != "" {
		w.EmployeeID = ""
		if db, ok := s.store.(managedBindings); ok {
			if err := db.CreateManaged(ctx, w, in.EmployeeID); err != nil {
				return nil, err
			}
		} else {
			if err := s.store.Save(ctx, w); err != nil {
				return nil, err
			}
			if in.EmployeeID != "" {
				if _, err := s.BindEmployee(ctx, w.ID, in.EmployeeID, actorID, ip); err != nil {
					_ = s.store.Delete(ctx, w.ID)
					return nil, err
				}
			}
		}
	} else if err := s.store.Save(ctx, w); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "workspace.create", "success", ip, map[string]string{"id": w.ID})
	return w, nil
}

// BindEmployee 为员工选择工作区。用户工作区保留其他员工的绑定。
func (s *Service) BindEmployee(ctx context.Context, workspaceID, employeeID, actorID, ip string) (*Workspace, error) {
	w, err := s.store.Get(ctx, workspaceID)
	if err != nil || w == nil {
		return nil, ErrNotFound
	}
	if w.OwnerUserID != "" {
		return s.changeManagedBinding(ctx, w, employeeID, false, actorID, ip)
	}
	if employeeID == "" {
		return s.UnbindEmployee(ctx, workspaceID, actorID, ip)
	}
	if s.binder != nil {
		if err := s.detachWorkspaceOwners(ctx, w, employeeID); err != nil {
			return nil, err
		}
		if err := s.detachEmployeeElsewhere(ctx, employeeID, workspaceID); err != nil {
			return nil, err
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

// UnbindEmployee 解除工作区与数字员工的绑定。
func (s *Service) UnbindEmployee(ctx context.Context, workspaceID, actorID, ip string) (*Workspace, error) {
	w, err := s.store.Get(ctx, workspaceID)
	if err != nil || w == nil {
		return nil, ErrNotFound
	}
	if s.binder != nil {
		owner, err := s.binder.FindByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		clear := map[string]struct{}{}
		if owner != "" {
			clear[owner] = struct{}{}
		}
		if w.EmployeeID != "" {
			clear[w.EmployeeID] = struct{}{}
		}
		for empID := range clear {
			if err := s.binder.BindWorkspace(ctx, empID, ""); err != nil {
				return nil, err
			}
		}
	}
	w.EmployeeID = ""
	w.UpdatedAt = time.Now().UTC()
	if err := s.store.Save(ctx, w); err != nil {
		return nil, err
	}
	s.audit.Log(ctx, "USER", actorID, "workspace.unbind", "success", ip, map[string]string{
		"workspace_id": workspaceID,
	})
	return w, nil
}

// detachWorkspaceOwners 换绑前解除当前挂在该工作区上的其他员工。
func (s *Service) detachWorkspaceOwners(ctx context.Context, w *Workspace, keepEmployee string) error {
	if s.binder == nil {
		return nil
	}
	owner, err := s.binder.FindByWorkspace(ctx, w.ID)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, id := range []string{owner, w.EmployeeID} {
		if id == "" || id == keepEmployee {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := s.binder.BindWorkspace(ctx, id, ""); err != nil {
			return err
		}
	}
	return nil
}

// detachEmployeeElsewhere 目标员工若还挂在别的工作区上，先从那边摘下来。
func (s *Service) detachEmployeeElsewhere(ctx context.Context, employeeID, keepWorkspace string) error {
	list, err := s.store.List(ctx)
	if err != nil {
		return err
	}
	for _, other := range list {
		if other == nil || other.ID == keepWorkspace || other.EmployeeID != employeeID {
			continue
		}
		other.EmployeeID = ""
		other.UpdatedAt = time.Now().UTC()
		if err := s.store.Save(ctx, other); err != nil {
			return err
		}
	}
	return nil
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

func (s *Service) Update(ctx context.Context, id, workstationID, root, path, repo, branch, actorID, ip string) (*Workspace, error) {
	w, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	changes := []string{
		audit.FormatChange("根目录", w.RootPath, pick(root, w.RootPath)),
		audit.FormatChange("工作站", w.WorkstationID, pick(workstationID, w.WorkstationID)),
		audit.FormatChange("路径", w.Path, pick(path, w.Path)),
		audit.FormatChange("仓库", w.Repository, repo),
		audit.FormatChange("分支", w.Branch, branch),
	}
	if root != "" && w.OwnerUserID != "" {
		w.RootPath = root
	}
	if workstationID != "" {
		w.WorkstationID = workstationID
	}
	if path != "" {
		w.Path = path
	}
	// 仓库和分支允许改成空，编辑表单会原样提交。
	w.Repository = repo
	w.Branch = branch
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
	w, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if w.OwnerUserID == "" {
		if _, err := s.UnbindEmployee(ctx, id, actorID, ip); err != nil {
			return err
		}
	} else if _, databaseHandlesBindings := s.store.(managedBindings); !databaseHandlesBindings && s.binder != nil {
		for {
			empID, err := s.binder.FindByWorkspace(ctx, id)
			if err != nil {
				return err
			}
			if empID == "" {
				break
			}
			if err := s.binder.BindWorkspace(ctx, empID, ""); err != nil {
				return err
			}
		}
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
	dirs map[string]string
	byID map[string]*Workspace
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{dirs: map[string]string{}, byID: map[string]*Workspace{}}
}

func (m *MemoryStore) Save(_ context.Context, w *Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w.OwnerUserID != "" {
		for _, other := range m.byID {
			if w.ID != other.ID && w.WorkstationID == other.WorkstationID && w.RootPath == other.RootPath && w.OwnerUserID == other.OwnerUserID && strings.EqualFold(w.Path, other.Path) {
				return errors.New("该项目目录已登记，请选择已有工作区")
			}
		}
	}
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

func (m *MemoryStore) UserDirectory(_ context.Context, id, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v := m.dirs[id]; v != "" {
		return v, nil
	}
	if !workspacedir.ValidName(name) {
		return "", errors.New("账号名不能用作用户目录")
	}
	for _, v := range m.dirs {
		if strings.EqualFold(v, name) {
			return "", errors.New("用户目录名已被占用")
		}
	}
	m.dirs[id] = name
	return name, nil
}

type managedBindings interface {
	CreateManaged(context.Context, *Workspace, string) error
	ChangeManagedBinding(context.Context, *Workspace, string, bool) error
}

func (s *Service) changeManagedBinding(ctx context.Context, w *Workspace, empID string, detach bool, actorID, ip string) (*Workspace, error) {
	if empID == "" {
		return nil, errors.New("请选择要绑定或解绑的员工")
	}
	if db, ok := s.store.(managedBindings); ok {
		if err := db.ChangeManagedBinding(ctx, w, empID, detach); err != nil {
			return nil, err
		}
	} else if s.binder != nil {
		target := w.ID
		if detach {
			target = ""
		}
		if err := s.binder.BindWorkspace(ctx, empID, target); err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("员工绑定服务不可用")
	}
	action := "workspace.bind"
	if detach {
		action = "workspace.unbind"
	}
	s.audit.Log(ctx, "USER", actorID, action, "success", ip, map[string]string{"workspace_id": w.ID, "employee_id": empID})
	return w, nil
}
func (s *Service) DetachManaged(ctx context.Context, w *Workspace, empID, actorID, ip string) (*Workspace, error) {
	return s.changeManagedBinding(ctx, w, empID, true, actorID, ip)
}
