// Package enrollment 负责 Workstation Enrollment Token 与注册签发。
// 设计依据：设计文档 §19、§125。
package enrollment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/certca"
)

// 错误。
var (
	ErrTokenInvalid = errors.New("enrollment token 无效或已过期")
	ErrTokenUsed    = errors.New("enrollment token 已使用")
)

// Token 一次性注册令牌。
type Token struct {
	ID        string
	Hash      string
	Label     string
	ExpiresAt time.Time
	UsedAt    time.Time
	CreatedBy string
}

// Store Token 存储。
type Store interface {
	Save(ctx context.Context, t *Token) error
	FindByHash(ctx context.Context, hash string) (*Token, error)
	MarkUsed(ctx context.Context, id string) error
}

// Auditor 审计。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// Service Enrollment 服务。
type Service struct {
	store Store
	ca    *certca.Authority
	audit Auditor
}

// NewService 创建 Enrollment 服务。
func NewService(store Store, ca *certca.Authority, audit Auditor) *Service {
	return &Service{store: store, ca: ca, audit: audit}
}

// CreateToken 签发一次性 token（明文仅返回一次）。
func (s *Service) CreateToken(ctx context.Context, label, createdBy, ip string, ttl time.Duration) (plain string, meta *Token, err error) {
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	plain = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plain))
	t := &Token{
		ID:        hex.EncodeToString(b[:8]),
		Hash:      hex.EncodeToString(sum[:]),
		Label:     label,
		ExpiresAt: time.Now().Add(ttl),
		CreatedBy: createdBy,
	}
	if err := s.store.Save(ctx, t); err != nil {
		return "", nil, err
	}
	s.audit.Log(ctx, "USER", createdBy, "enrollment.create_token", "success", ip, map[string]string{"label": label})
	return plain, t, nil
}

// Enroll 校验 token、签发客户端证书。返回 CA PEM、客户端证书 PEM。
func (s *Service) Enroll(ctx context.Context, plainToken, workstationID string, csrPEM []byte, ip string) (caPEM, certPEM string, rec *certca.Record, err error) {
	sum := sha256.Sum256([]byte(plainToken))
	hash := hex.EncodeToString(sum[:])
	t, err := s.store.FindByHash(ctx, hash)
	if err != nil || t == nil {
		s.audit.Log(ctx, "WORKSTATION", workstationID, "enrollment.enroll", "failed_token", ip, nil)
		return "", "", nil, ErrTokenInvalid
	}
	if !t.UsedAt.IsZero() {
		return "", "", nil, ErrTokenUsed
	}
	if time.Now().After(t.ExpiresAt) {
		return "", "", nil, ErrTokenInvalid
	}
	rec, err = s.ca.SignCSR(workstationID, csrPEM, 365)
	if err != nil {
		s.audit.Log(ctx, "WORKSTATION", workstationID, "enrollment.enroll", "failed_sign", ip, nil)
		return "", "", nil, err
	}
	_ = s.store.MarkUsed(ctx, t.ID)
	s.audit.Log(ctx, "WORKSTATION", workstationID, "enrollment.enroll", "success", ip, map[string]string{"fingerprint": rec.Fingerprint})
	return string(s.ca.CAPEM()), rec.CertPEM, rec, nil
}

// MemoryStore 内存 Token 库。
type MemoryStore struct {
	mu   sync.RWMutex
	byH  map[string]*Token
	byID map[string]*Token
}

// NewMemoryStore 创建内存 Token 存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{byH: map[string]*Token{}, byID: map[string]*Token{}}
}

// Save 保存 token。
func (m *MemoryStore) Save(_ context.Context, t *Token) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *t
	m.byH[t.Hash] = &cp
	m.byID[t.ID] = &cp
	return nil
}

// FindByHash 按哈希查找。
func (m *MemoryStore) FindByHash(_ context.Context, hash string) (*Token, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t := m.byH[hash]
	if t == nil {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

// MarkUsed 标记已使用。
func (m *MemoryStore) MarkUsed(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.byID[id]
	if t == nil {
		return ErrTokenInvalid
	}
	t.UsedAt = time.Now().UTC()
	if h := m.byH[t.Hash]; h != nil {
		h.UsedAt = t.UsedAt
	}
	return nil
}
