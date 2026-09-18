// Package artifact Job 产物元数据与本地存储（SHA256 去重）。
// 设计依据：设计文档 §63、§96。
package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// 错误。
var (
	ErrNotFound = errors.New("artifact 不存在")
)

// Artifact 元数据。
type Artifact struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	Storage   string    `json:"storage"`
	CreatedAt time.Time `json:"created_at"`
}

// Store 元数据。
type Store interface {
	Save(ctx context.Context, a *Artifact) error
	Get(ctx context.Context, id string) (*Artifact, error)
	GetByHash(ctx context.Context, hash string) (*Artifact, error)
	ListByJob(ctx context.Context, jobID string) ([]*Artifact, error)
	List(ctx context.Context) ([]*Artifact, error)
}

// Service 产物服务。
type Service struct {
	store Store
	root  string
}

// New 创建。
func New(store Store, root string) *Service {
	return &Service{store: store, root: root}
}

// Put 写入并登记；相同 SHA256 去重引用。返回 (artifact, deduped, err)。
func (s *Service) Put(ctx context.Context, jobID, name, typ string, r io.Reader) (*Artifact, bool, error) {
	if jobID == "" || name == "" {
		return nil, false, errors.New("需要 job_id/name")
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, false, err
	}
	sumArr := sha256.Sum256(data)
	sum := hex.EncodeToString(sumArr[:])

	if existing, _ := s.store.GetByHash(ctx, sum); existing != nil {
		a := &Artifact{
			ID: idgen.New("ART"), JobID: jobID, Name: name, Type: typ,
			SizeBytes: int64(len(data)), SHA256: sum, Storage: existing.Storage,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.store.Save(ctx, a); err != nil {
			return nil, false, err
		}
		return a, true, nil
	}

	storage := "mem:" + sum
	if s.root != "" {
		if err := os.MkdirAll(s.root, 0o755); err != nil {
			return nil, false, err
		}
		final := filepath.Join(s.root, sum)
		if err := os.WriteFile(final, data, 0o600); err != nil {
			return nil, false, err
		}
		storage = "local:" + final
	} else if ms, ok := s.store.(*MemoryStore); ok {
		ms.mu.Lock()
		ms.blobs[sum] = append([]byte{}, data...)
		ms.mu.Unlock()
	}

	a := &Artifact{
		ID: idgen.New("ART"), JobID: jobID, Name: name, Type: typ,
		SizeBytes: int64(len(data)), SHA256: sum, Storage: storage,
		CreatedAt: time.Now().UTC(),
	}
	if err := s.store.Save(ctx, a); err != nil {
		return nil, false, err
	}
	return a, false, nil
}

// Open 打开内容。
func (s *Service) Open(ctx context.Context, id string) (io.ReadCloser, *Artifact, error) {
	a, err := s.store.Get(ctx, id)
	if err != nil || a == nil {
		return nil, nil, ErrNotFound
	}
	if ms, ok := s.store.(*MemoryStore); ok {
		ms.mu.RLock()
		b := append([]byte{}, ms.blobs[a.SHA256]...)
		ms.mu.RUnlock()
		return io.NopCloser(bytes.NewReader(b)), a, nil
	}
	path := a.Storage
	if len(path) > 6 && path[:6] == "local:" {
		path = path[6:]
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, a, nil
}

func (s *Service) Get(ctx context.Context, id string) (*Artifact, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context) ([]*Artifact, error) {
	return s.store.List(ctx)
}
func (s *Service) ListByJob(ctx context.Context, jobID string) ([]*Artifact, error) {
	return s.store.ListByJob(ctx, jobID)
}

// MemoryStore 内存。
type MemoryStore struct {
	mu     sync.RWMutex
	byID   map[string]*Artifact
	byHash map[string]*Artifact
	blobs  map[string][]byte
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID: map[string]*Artifact{}, byHash: map[string]*Artifact{}, blobs: map[string][]byte{},
	}
}

func (m *MemoryStore) Save(_ context.Context, a *Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *a
	m.byID[a.ID] = &cp
	if _, ok := m.byHash[a.SHA256]; !ok {
		m.byHash[a.SHA256] = &cp
	}
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *MemoryStore) GetByHash(_ context.Context, hash string) (*Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.byHash[hash]
	if !ok {
		return nil, nil
	}
	cp := *a
	return &cp, nil
}

func (m *MemoryStore) ListByJob(_ context.Context, jobID string) ([]*Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Artifact
	for _, a := range m.byID {
		if a.JobID == jobID {
			cp := *a
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*Artifact, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Artifact, 0, len(m.byID))
	for _, a := range m.byID {
		cp := *a
		out = append(out, &cp)
	}
	return out, nil
}
