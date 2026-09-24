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
	"strings"
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
	ID            string    `json:"id"`
	JobID         string    `json:"job_id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	SizeBytes     int64     `json:"size_bytes"`
	SHA256        string    `json:"sha256"`
	Storage       string    `json:"storage"`
	UploadedBy    string    `json:"uploaded_by,omitempty"`    // admin:<user> | workstation
	WorkstationID string    `json:"workstation_id,omitempty"` // 工作站上传时必填
	CreatedAt     time.Time `json:"created_at"`
}

// Store 元数据。
type Store interface {
	Save(ctx context.Context, a *Artifact) error
	Get(ctx context.Context, id string) (*Artifact, error)
	GetByHash(ctx context.Context, hash string) (*Artifact, error)
	ListByJob(ctx context.Context, jobID string) ([]*Artifact, error)
	List(ctx context.Context) ([]*Artifact, error)
}

// JobInfo 上传鉴权所需的最小 Job 视图。
type JobInfo struct {
	ID            string
	WorkstationID string
	Status        string
}

// JobLookup 查询 Job（解耦 job 包）。
type JobLookup interface {
	LookupJob(ctx context.Context, id string) (*JobInfo, error)
}

// PutInput 安全上传入参。
type PutInput struct {
	JobID         string
	Name          string
	Type          string
	Body          io.Reader
	MaxBytes      int64  // 0 = DefaultMaxBytes
	UploadedBy    string // admin:uid / workstation
	WorkstationID string // 工作站上传时必填，须与 Job.WorkstationID 一致
	RequireWSBind bool   // true：强制 Job 已分配给该工作站
}

// Service 产物服务。
type Service struct {
	store Store
	root  string
	jobs  JobLookup
}

// New 创建；jobs 可为 nil（仅测试内存路径，生产必须注入）。
func New(store Store, root string) *Service {
	return &Service{store: store, root: root}
}

// SetJobLookup 注入 Job 查询（启动时接线）。
func (s *Service) SetJobLookup(j JobLookup) {
	s.jobs = j
}

// Put 兼容旧调用：无策略校验，测试用；生产请用 PutSecure。
func (s *Service) Put(ctx context.Context, jobID, name, typ string, r io.Reader) (*Artifact, bool, error) {
	return s.PutSecure(ctx, PutInput{
		JobID: jobID, Name: name, Type: typ, Body: r,
		UploadedBy: "legacy",
	})
}

// PutSecure 写入并登记；校验 Job/文件名/类型/大小；相同 SHA256 去重引用。
func (s *Service) PutSecure(ctx context.Context, in PutInput) (*Artifact, bool, error) {
	if in.JobID == "" {
		return nil, false, ErrJobRequired
	}
	name, err := SanitizeName(in.Name)
	if err != nil {
		return nil, false, err
	}
	typ, err := NormalizeType(in.Type)
	if err != nil {
		return nil, false, err
	}
	max := in.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}

	if s.jobs != nil {
		j, err := s.jobs.LookupJob(ctx, in.JobID)
		if err != nil || j == nil {
			return nil, false, ErrJobRequired
		}
		if in.RequireWSBind {
			if in.WorkstationID == "" || j.WorkstationID == "" || j.WorkstationID != in.WorkstationID {
				return nil, false, ErrJobForbidden
			}
		}
	} else if in.RequireWSBind {
		return nil, false, ErrJobForbidden
	}

	limited := io.LimitReader(in.Body, max+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > max {
		return nil, false, ErrTooLarge
	}
	if len(data) == 0 {
		return nil, false, ErrEmptyBody
	}

	sumArr := sha256.Sum256(data)
	sum := hex.EncodeToString(sumArr[:])

	if existing, _ := s.store.GetByHash(ctx, sum); existing != nil {
		a := &Artifact{
			ID: idgen.New("ART"), JobID: in.JobID, Name: name, Type: typ,
			SizeBytes: int64(len(data)), SHA256: sum, Storage: existing.Storage,
			UploadedBy: in.UploadedBy, WorkstationID: in.WorkstationID,
			CreatedAt: time.Now().UTC(),
		}
		if err := s.store.Save(ctx, a); err != nil {
			return nil, false, err
		}
		return a, true, nil
	}

	storage := "mem:" + sum
	if s.root != "" {
		if err := os.MkdirAll(s.root, 0o750); err != nil {
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
		ID: idgen.New("ART"), JobID: in.JobID, Name: name, Type: typ,
		SizeBytes: int64(len(data)), SHA256: sum, Storage: storage,
		UploadedBy: in.UploadedBy, WorkstationID: in.WorkstationID,
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
	storage := a.Storage
	if strings.HasPrefix(storage, "local:") {
		path := storage[len("local:"):]
		f, err := os.Open(path)
		if err != nil {
			return nil, nil, err
		}
		return f, a, nil
	}
	if strings.HasPrefix(storage, "mem:") {
		if ms, ok := s.store.(*MemoryStore); ok {
			ms.mu.RLock()
			b := append([]byte{}, ms.blobs[a.SHA256]...)
			ms.mu.RUnlock()
			return io.NopCloser(bytes.NewReader(b)), a, nil
		}
	}
	// 兼容：MemoryStore 且 storage 未带前缀
	if ms, ok := s.store.(*MemoryStore); ok {
		ms.mu.RLock()
		b := append([]byte{}, ms.blobs[a.SHA256]...)
		ms.mu.RUnlock()
		if len(b) > 0 {
			return io.NopCloser(bytes.NewReader(b)), a, nil
		}
	}
	return nil, nil, ErrNotFound
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
