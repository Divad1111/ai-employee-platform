// Package secret 密钥引用存储与 SecretManager。
// 明文不进 Prompt / Job / Message / config.yaml / 普通 Application Log。
// 设计依据：设计文档 §62、§88。
package secret

import (
	"context"
	"errors"
	"sync"
	"time"
)

// 扩展错误。
var (
	ErrResolveFailed = errors.New("Secret 解析失败")
	ErrNotBound     = errors.New("Employee 未绑定该 Secret")
)

// Meta 可安全返回 Admin 的元数据（无明文）。
type Meta struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Masked      string    `json:"masked"` // 恒为 ***
	CreatedAt   time.Time `json:"created_at"`
	RotatedAt   time.Time `json:"rotated_at,omitempty"`
	CreatedBy   string    `json:"created_by,omitempty"`
}

// Binding Employee → Secret 引用。
type Binding struct {
	ID         string    `json:"id"`
	EmployeeID string    `json:"employee_id"`
	SecretID   string    `json:"secret_id"`
	Purpose    string    `json:"purpose"` // 如 GIT_TOKEN / NPM_TOKEN / ENV:FOO
	CreatedAt  time.Time `json:"created_at"`
}

// Resolved 运行时注入条目（仅受控路径持有明文，禁止记日志）。
type Resolved struct {
	EnvKey   string // 环境变量名
	SecretID string
	// Value 明文；调用方不得写入普通日志
	Value string `json:"-"`
}

// Auditor 审计（无明文）。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// BindingStore 绑定持久化。
type BindingStore interface {
	Save(ctx context.Context, b *Binding) error
	ListByEmployee(ctx context.Context, employeeID string) ([]*Binding, error)
	ListBySecret(ctx context.Context, secretID string) ([]*Binding, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]*Binding, error)
}

// Manager SecretManager：引用模型 + 受控解析 + Audit。
type Manager struct {
	store Store
	bind  BindingStore
	audit Auditor
	meta  map[string]*Meta // id → 元数据
	mu    sync.RWMutex
}

// NewManager 创建。
func NewManager(store Store, bind BindingStore, audit Auditor) *Manager {
	return &Manager{
		store: store, bind: bind, audit: audit,
		meta: map[string]*Meta{},
	}
}

// Put 写入密文，返回引用（无明文）。
func (m *Manager) Put(ctx context.Context, name, plaintext, actorID, ip, desc string) (Meta, error) {
	ref, err := m.store.Put(name, plaintext)
	if err != nil {
		return Meta{}, err
	}
	now := time.Now().UTC()
	meta := &Meta{
		ID: ref.ID, Name: name, Description: desc,
		Masked: "***", CreatedAt: now, CreatedBy: actorID,
	}
	m.mu.Lock()
	m.meta[ref.ID] = meta
	m.mu.Unlock()
	if m.audit != nil {
		m.audit.Log(ctx, "USER", actorID, "secret.put", "success", ip, map[string]string{
			"secret_id": ref.ID, "name": name, "value": Redact(plaintext),
		})
	}
	cp := *meta
	return cp, nil
}

// Rotate 轮换明文；调用方须先过 CRITICAL/TOTP。
func (m *Manager) Rotate(ctx context.Context, id, newPlaintext, actorID, ip string) (Meta, error) {
	old, err := m.store.Ref(id)
	if err != nil {
		return Meta{}, err
	}
	// 覆盖：同名再 Put 会产生新 ID；此处用底层替换
	if rv, ok := m.store.(Rotator); ok {
		if err := rv.Replace(id, newPlaintext); err != nil {
			return Meta{}, err
		}
	} else {
		// 回退：删除语义不可用时新建并更新绑定 — 简化为 Replace 必须实现
		return Meta{}, errors.New("当前 Vault 不支持 Rotate")
	}
	m.mu.Lock()
	meta, ok := m.meta[id]
	if !ok {
		meta = &Meta{ID: id, Name: old.Name, Masked: "***", CreatedAt: time.Now().UTC()}
		m.meta[id] = meta
	}
	meta.RotatedAt = time.Now().UTC()
	cp := *meta
	m.mu.Unlock()
	if m.audit != nil {
		m.audit.Log(ctx, "USER", actorID, "secret.rotate", "success", ip, map[string]string{
			"secret_id": id, "name": old.Name, "value": Redact(newPlaintext),
		})
	}
	return cp, nil
}

// Rotator 可原地替换密文。
type Rotator interface {
	Replace(id, plaintext string) error
	Delete(id string) error
}

// Access 受控解密；写 Secret Access Audit，永不返回到普通日志路径由调用方保证。
func (m *Manager) Access(ctx context.Context, secretID, actorType, actorID, ip, purpose string) (string, error) {
	pt, err := m.store.Get(secretID)
	result := "success"
	if err != nil {
		result = "failed"
		if m.audit != nil {
			m.audit.Log(ctx, actorType, actorID, "secret.access", result, ip, map[string]string{
				"secret_id": secretID, "purpose": purpose, "error": "resolve_failed",
			})
		}
		return "", ErrResolveFailed
	}
	if m.audit != nil {
		m.audit.Log(ctx, actorType, actorID, "secret.access", result, ip, map[string]string{
			"secret_id": secretID, "purpose": purpose,
		})
	}
	return pt, nil
}

// Bind 将 Secret 引用绑定到 Employee。
func (m *Manager) Bind(ctx context.Context, employeeID, secretID, purpose, actorID, ip string) (*Binding, error) {
	if _, err := m.store.Ref(secretID); err != nil {
		return nil, err
	}
	if purpose == "" {
		purpose = "DEFAULT"
	}
	b := &Binding{
		ID: newID(), EmployeeID: employeeID, SecretID: secretID,
		Purpose: purpose, CreatedAt: time.Now().UTC(),
	}
	if err := m.bind.Save(ctx, b); err != nil {
		return nil, err
	}
	if m.audit != nil {
		m.audit.Log(ctx, "USER", actorID, "secret.bind", "success", ip, map[string]string{
			"employee_id": employeeID, "secret_id": secretID, "purpose": purpose,
		})
	}
	return b, nil
}

// ResolveForEmployee 解析 Employee 全部绑定（供 Job/Session 注入）；失败不回显明文。
func (m *Manager) ResolveForEmployee(ctx context.Context, employeeID, actorType, actorID, ip string) ([]Resolved, error) {
	list, err := m.bind.ListByEmployee(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]Resolved, 0, len(list))
	for _, b := range list {
		pt, err := m.Access(ctx, b.SecretID, actorType, actorID, ip, b.Purpose)
		if err != nil {
			return nil, ErrResolveFailed
		}
		envKey := purposeToEnv(b.Purpose)
		out = append(out, Resolved{EnvKey: envKey, SecretID: b.SecretID, Value: pt})
	}
	return out, nil
}

func purposeToEnv(purpose string) string {
	if len(purpose) > 4 && purpose[:4] == "ENV:" {
		return purpose[4:]
	}
	switch purpose {
	case "GIT_TOKEN":
		return "AIE_GIT_TOKEN"
	case "NPM_TOKEN":
		return "AIE_NPM_TOKEN"
	case "SSH_KEY":
		return "AIE_SSH_KEY"
	default:
		return "AIE_SECRET_" + purpose
	}
}

// ListMeta 列表元数据（无明文）。
func (m *Manager) ListMeta() []Meta {
	refs := m.store.List()
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Meta, 0, len(refs))
	for _, r := range refs {
		if meta, ok := m.meta[r.ID]; ok {
			cp := *meta
			out = append(out, cp)
			continue
		}
		out = append(out, Meta{ID: r.ID, Name: r.Name, Masked: "***"})
	}
	return out
}

// GetMeta 单条元数据。
func (m *Manager) GetMeta(id string) (Meta, error) {
	if _, err := m.store.Ref(id); err != nil {
		return Meta{}, err
	}
	m.mu.RLock()
	meta, ok := m.meta[id]
	m.mu.RUnlock()
	if ok {
		cp := *meta
		return cp, nil
	}
	ref, _ := m.store.Ref(id)
	return Meta{ID: id, Name: ref.Name, Masked: "***"}, nil
}

// Delete 删除（需 step-up，由 API 层保证）。
func (m *Manager) Delete(ctx context.Context, id, actorID, ip string) error {
	if rv, ok := m.store.(Rotator); ok {
		if err := rv.Delete(id); err != nil {
			return err
		}
	} else {
		return errors.New("当前 Vault 不支持 Delete")
	}
	m.mu.Lock()
	delete(m.meta, id)
	m.mu.Unlock()
	if m.audit != nil {
		m.audit.Log(ctx, "USER", actorID, "secret.delete", "success", ip, map[string]string{"secret_id": id})
	}
	return nil
}

// Store 底层访问（Feishu 等既有路径）。
func (m *Manager) Store() Store { return m.store }

// MemoryBindings 内存绑定。
type MemoryBindings struct {
	mu   sync.RWMutex
	byID map[string]*Binding
}

func NewMemoryBindings() *MemoryBindings {
	return &MemoryBindings{byID: map[string]*Binding{}}
}

func (m *MemoryBindings) Save(_ context.Context, b *Binding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *b
	m.byID[b.ID] = &cp
	return nil
}

func (m *MemoryBindings) ListByEmployee(_ context.Context, employeeID string) ([]*Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Binding
	for _, b := range m.byID {
		if b.EmployeeID == employeeID {
			cp := *b
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryBindings) ListBySecret(_ context.Context, secretID string) ([]*Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Binding
	for _, b := range m.byID {
		if b.SecretID == secretID {
			cp := *b
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryBindings) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.byID, id)
	return nil
}

func (m *MemoryBindings) List(_ context.Context) ([]*Binding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Binding, 0, len(m.byID))
	for _, b := range m.byID {
		cp := *b
		out = append(out, &cp)
	}
	return out, nil
}
