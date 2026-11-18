// Package mcpauth 管理「工作流MCP」专用 Token（aiemcp_ 前缀）。
// 支持 EMPLOYEE（工作流授权过滤）、USER（走 RBAC）与 EXTERNAL_USER（显式外部授权）。
package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

const (
	Prefix = "aiemcp_"

	SubjectEmployee = "EMPLOYEE"
	SubjectUser     = "USER"
	// SubjectExternalUser is delegated through an authenticated management session.
	// It is accepted only by the MCP endpoint, never as a management session.
	SubjectExternalUser = "EXTERNAL_USER"

	ScopeRead  = "READ"
	ScopeWrite = "WRITE"
	ScopeAdmin = "ADMIN"
)

var (
	ErrNotFound     = errors.New("token 不存在")
	ErrInvalidToken = errors.New("无效 token")
	ErrRevoked      = errors.New("token 已吊销")
	ErrExpired      = errors.New("token 已过期")
)

// Token MCP Token 元数据（不含明文）。
type Token struct {
	ID          string     `json:"id"`
	Service     string     `json:"service"`
	TokenHash   string     `json:"-"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	Scope       string     `json:"scope"`
	Label       string     `json:"label"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedBy   string     `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
}

// IssueResult 签发结果（明文仅返回一次）。
type IssueResult struct {
	Token  *Token `json:"token"`
	Secret string `json:"secret"` // 完整 aiemcp_xxx，仅创建时返回
}

// Store Token 持久化。
type Store interface {
	Save(ctx context.Context, t *Token) error
	GetByHash(ctx context.Context, hash string) (*Token, error)
	GetByID(ctx context.Context, id string) (*Token, error)
	ListBySubject(ctx context.Context, subjectType, subjectID string) ([]*Token, error)
	Update(ctx context.Context, t *Token) error
	Delete(ctx context.Context, id string) error
	GetExternalPolicy(ctx context.Context, userID string) (*ExternalPolicy, error)
	SetExternalPolicy(ctx context.Context, policy *ExternalPolicy) error
	SetExternalServicePolicy(ctx context.Context, userID, service string, canWrite bool, actorID string) (*ExternalPolicy, error)
}

// Service Token 业务服务。
type Service struct {
	store Store
}

// NewService 创建服务。
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Issue 签发 Token。
func (s *Service) Issue(ctx context.Context, subjectType, subjectID, scope, label, createdBy string, expiresIn time.Duration) (*IssueResult, error) {
	return s.IssueFor(ctx, Workflow, subjectType, subjectID, scope, label, createdBy, expiresIn)
}

func (s *Service) IssueFor(ctx context.Context, service, subjectType, subjectID, scope, label, createdBy string, expiresIn time.Duration) (*IssueResult, error) {
	if _, ok := FindBuiltin(service); !ok || (service != Workflow && service != Collector) {
		return nil, fmt.Errorf("%w: service", ErrInvalidToken)
	}
	if subjectType != SubjectEmployee && subjectType != SubjectUser && subjectType != SubjectExternalUser {
		return nil, fmt.Errorf("%w: subject_type", ErrInvalidToken)
	}
	if subjectType == SubjectExternalUser && (subjectID == "" || createdBy != subjectID || expiresIn <= 0 || (scope != ScopeRead && scope != ScopeWrite)) {
		return nil, fmt.Errorf("%w: external authorization", ErrInvalidToken)
	}
	if subjectType == SubjectExternalUser && scope == ScopeWrite {
		policy, err := s.ExternalPolicy(ctx, subjectID)
		if err != nil {
			return nil, err
		}
		if !policy.CanWriteService(service) {
			return nil, fmt.Errorf("%w: 未获管理员授予外部 MCP 写权限", ErrInvalidToken)
		}
	}
	if scope == "" {
		if subjectType == SubjectEmployee {
			scope = ScopeRead
		} else {
			scope = ScopeAdmin
		}
	}
	secret, hash, err := generateSecret()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	t := &Token{
		Service:     service,
		ID:          idgen.New("MCP"),
		TokenHash:   hash,
		SubjectType: subjectType,
		SubjectID:   subjectID,
		Scope:       scope,
		Label:       label,
		CreatedBy:   createdBy,
		CreatedAt:   now,
	}
	if expiresIn > 0 {
		exp := now.Add(expiresIn)
		t.ExpiresAt = &exp
	}
	if err := s.store.Save(ctx, t); err != nil {
		return nil, err
	}
	return &IssueResult{Token: t, Secret: secret}, nil
}

// Validate 校验 Bearer Token，成功返回元数据并更新 last_used_at。
func (s *Service) Validate(ctx context.Context, raw string) (*Token, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, Prefix) {
		return nil, ErrInvalidToken
	}
	hash := hashToken(raw)
	t, err := s.store.GetByHash(ctx, hash)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if t.RevokedAt != nil {
		return nil, ErrRevoked
	}
	if t.ExpiresAt != nil && time.Now().UTC().After(*t.ExpiresAt) {
		return nil, ErrExpired
	}
	now := time.Now().UTC()
	t.LastUsedAt = &now
	_ = s.store.Update(ctx, t)
	return t, nil
}

// Get 按 ID 读取 Token 元数据。
func (s *Service) Get(ctx context.Context, id string) (*Token, error) {
	if s == nil || s.store == nil {
		return nil, ErrNotFound
	}
	return s.store.GetByID(ctx, id)
}

// Revoke 吊销 Token。
func (s *Service) Revoke(ctx context.Context, id string) error {
	t, err := s.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	return s.store.Update(ctx, t)
}

// ListBySubject 列出主体的 Token。
func (s *Service) ListBySubject(ctx context.Context, subjectType, subjectID string) ([]*Token, error) {
	return s.store.ListBySubject(ctx, subjectType, subjectID)
}

// Delete 物理删除。
func (s *Service) Delete(ctx context.Context, id string) error {
	return s.store.Delete(ctx, id)
}

func generateSecret() (secret, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	secret = Prefix + hex.EncodeToString(b)
	hash = hashToken(secret)
	return secret, hash, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ---------- Memory Store ----------

// MemoryStore 内存 Token 存储。
type MemoryStore struct {
	mu       sync.RWMutex
	byID     map[string]*Token
	byHash   map[string]*Token
	policies map[string]*ExternalPolicy
}

// NewMemoryStore 创建空存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID:     map[string]*Token{},
		byHash:   map[string]*Token{},
		policies: map[string]*ExternalPolicy{},
	}
}

func (m *MemoryStore) Save(_ context.Context, t *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.byID[t.ID] = &cp
	m.byHash[t.TokenHash] = &cp
	return nil
}

func (m *MemoryStore) GetByHash(_ context.Context, hash string) (*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.byHash[hash]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *MemoryStore) GetByID(_ context.Context, id string) (*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *MemoryStore) ListBySubject(_ context.Context, subjectType, subjectID string) ([]*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Token
	for _, t := range m.byID {
		if t.SubjectType == subjectType && t.SubjectID == subjectID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) Update(_ context.Context, t *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.byID[t.ID]; !ok {
		return ErrNotFound
	}
	cp := *t
	// A validation request that started before Revoke must never revive a token
	// when it records last_used_at with an older snapshot.
	if m.byID[t.ID].RevokedAt != nil {
		cp.RevokedAt = m.byID[t.ID].RevokedAt
	}
	m.byID[t.ID] = &cp
	m.byHash[t.TokenHash] = &cp
	return nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.byHash, t.TokenHash)
	delete(m.byID, id)
	return nil
}

var _ Store = (*MemoryStore)(nil)
