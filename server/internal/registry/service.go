// Package registry Provider 版本注册表与签名校验。
// Q-06：**B）Control Plane 下发公钥**（Workstation 可缓存；开发可内置 fallback）。
// 设计依据：设计文档 §38、§95、§96。
package registry

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// 安装状态。
const (
	StatusNotInstalled = "NOT_INSTALLED"
	StatusInstalling   = "INSTALLING"
	StatusInstalled    = "INSTALLED"
	StatusError        = "ERROR"
)

// 错误。
var (
	ErrNotFound       = errors.New("provider/version 不存在")
	ErrBadChecksum    = errors.New("SHA256 校验失败")
	ErrBadSignature   = errors.New("签名校验失败")
	ErrNoSigningKey   = errors.New("未配置签名公钥")
)

// Provider 目录项。
type Provider struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Capabilities map[string]bool   `json:"capabilities"` // acp/mcp/headless
}

// Version 版本包。
type Version struct {
	ID           string    `json:"id"`
	ProviderID   string    `json:"provider_id"`
	Version      string    `json:"version"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	DownloadURL  string    `json:"download_url"`
	SHA256       string    `json:"sha256"`
	Signature    string    `json:"signature"` // base64 ed25519(sig of sha256 hex bytes or raw hash)
	ReleaseNotes string    `json:"release_notes"`
	CreatedAt    time.Time `json:"created_at"`
}

// Installation 安装记录。
type Installation struct {
	ID            string `json:"id"`
	WorkstationID string `json:"workstation_id"`
	ProviderID    string `json:"provider_id"`
	Version       string `json:"version"`
	Status        string `json:"status"`
}

// Store 持久化。
type Store interface {
	SaveProvider(ctx context.Context, p *Provider) error
	ListProviders(ctx context.Context) ([]*Provider, error)
	GetProvider(ctx context.Context, id string) (*Provider, error)
	SaveVersion(ctx context.Context, v *Version) error
	ListVersions(ctx context.Context, providerID, os, arch string) ([]*Version, error)
	GetVersion(ctx context.Context, id string) (*Version, error)
	SaveInstall(ctx context.Context, i *Installation) error
	GetInstall(ctx context.Context, wsID, providerID string) (*Installation, error)
}

// Service 注册表。
type Service struct {
	store      Store
	pubKey     ed25519.PublicKey // CP 下发/配置
	pubKeyB64  string
}

// New 创建；pubKeyB64 为空则生成开发密钥对（仅测试）。
func New(store Store, pubKeyB64 string) (*Service, ed25519.PrivateKey, error) {
	s := &Service{store: store}
	if pubKeyB64 != "" {
		raw, err := base64.StdEncoding.DecodeString(pubKeyB64)
		if err != nil {
			return nil, nil, err
		}
		s.pubKey = ed25519.PublicKey(raw)
		s.pubKeyB64 = pubKeyB64
		return s, nil, nil
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, nil, err
	}
	s.pubKey = pub
	s.pubKeyB64 = base64.StdEncoding.EncodeToString(pub)
	return s, priv, nil
}

// SigningPublicKey 供 Workstation 拉取（Q-06 B）。
func (s *Service) SigningPublicKey() string { return s.pubKeyB64 }

// UpsertProvider 登记 Provider。
func (s *Service) UpsertProvider(ctx context.Context, p *Provider) error {
	if p.ID == "" {
		return errors.New("需要 id")
	}
	if p.Capabilities == nil {
		p.Capabilities = map[string]bool{"acp": true}
	}
	return s.store.SaveProvider(ctx, p)
}

// AddVersion 登记版本。
func (s *Service) AddVersion(ctx context.Context, v *Version) (*Version, error) {
	if v.ProviderID == "" || v.Version == "" || v.SHA256 == "" {
		return nil, errors.New("需要 provider_id/version/sha256")
	}
	if v.ID == "" {
		v.ID = idgen.Raw()
	}
	v.CreatedAt = time.Now().UTC()
	if err := s.store.SaveVersion(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// QueryVersions 按 OS/Arch 查询。
func (s *Service) QueryVersions(ctx context.Context, providerID, osName, arch string) ([]*Version, error) {
	return s.store.ListVersions(ctx, providerID, osName, arch)
}

// VerifyPackage 校验包内容：SHA256 + ed25519 签名。
func (s *Service) VerifyPackage(content []byte, expectSHA256Hex, signatureB64 string) error {
	sum := sha256.Sum256(content)
	got := hex.EncodeToString(sum[:])
	if expectSHA256Hex != "" && got != expectSHA256Hex {
		return ErrBadChecksum
	}
	if s.pubKey == nil {
		return ErrNoSigningKey
	}
	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return ErrBadSignature
	}
	// 签名对象：原始 sha256 摘要 32 字节
	if !ed25519.Verify(s.pubKey, sum[:], sig) {
		return ErrBadSignature
	}
	return nil
}

// SignHash 测试辅助：用私钥签内容。
func SignContent(priv ed25519.PrivateKey, content []byte) (shaHex, sigB64 string) {
	sum := sha256.Sum256(content)
	sig := ed25519.Sign(priv, sum[:])
	return hex.EncodeToString(sum[:]), base64.StdEncoding.EncodeToString(sig)
}

// SetInstallStatus 更新安装状态。
func (s *Service) SetInstallStatus(ctx context.Context, wsID, providerID, version, status string) (*Installation, error) {
	inst := &Installation{
		ID: idgen.Raw(), WorkstationID: wsID, ProviderID: providerID,
		Version: version, Status: status,
	}
	if err := s.store.SaveInstall(ctx, inst); err != nil {
		return nil, err
	}
	return inst, nil
}

// ListProviders / GetProvider。
func (s *Service) ListProviders(ctx context.Context) ([]*Provider, error) {
	return s.store.ListProviders(ctx)
}

// MemoryStore。
type MemoryStore struct {
	mu        sync.RWMutex
	providers map[string]*Provider
	versions  map[string]*Version
	installs  map[string]*Installation // wsID|providerID
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		providers: map[string]*Provider{},
		versions:  map[string]*Version{},
		installs:  map[string]*Installation{},
	}
}

func (m *MemoryStore) SaveProvider(_ context.Context, p *Provider) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	if p.Capabilities != nil {
		cp.Capabilities = map[string]bool{}
		for k, v := range p.Capabilities {
			cp.Capabilities[k] = v
		}
	}
	m.providers[p.ID] = &cp
	return nil
}

func (m *MemoryStore) ListProviders(_ context.Context) ([]*Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Provider, 0, len(m.providers))
	for _, p := range m.providers {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) GetProvider(_ context.Context, id string) (*Provider, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.providers[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

func (m *MemoryStore) SaveVersion(_ context.Context, v *Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *v
	m.versions[v.ID] = &cp
	return nil
}

func (m *MemoryStore) ListVersions(_ context.Context, providerID, osName, arch string) ([]*Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Version
	for _, v := range m.versions {
		if providerID != "" && v.ProviderID != providerID {
			continue
		}
		if osName != "" && v.OS != osName {
			continue
		}
		if arch != "" && v.Arch != arch {
			continue
		}
		cp := *v
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) GetVersion(_ context.Context, id string) (*Version, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.versions[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *MemoryStore) SaveInstall(_ context.Context, i *Installation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *i
	m.installs[i.WorkstationID+"|"+i.ProviderID] = &cp
	return nil
}

func (m *MemoryStore) GetInstall(_ context.Context, wsID, providerID string) (*Installation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	i, ok := m.installs[wsID+"|"+providerID]
	if !ok {
		return nil, nil
	}
	cp := *i
	return &cp, nil
}
