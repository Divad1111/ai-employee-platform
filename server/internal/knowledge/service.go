// Package knowledge Employee 知识上下文。
// 设计依据：设计文档 §8、§33。
package knowledge

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// 错误。
var (
	ErrNotFound     = errors.New("knowledge 不存在")
	ErrInvalidInput = errors.New("参数无效")
)

// Entry 知识条目。
type Entry struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	Content     string    `json:"content"`
	Tags        []string  `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Service 内存知识服务。
type Service struct {
	mu       sync.RWMutex
	entries  map[string]*Entry
	bindings map[string]map[string]struct{} // employee → knowledge
}

// NewService 创建。
func NewService() *Service {
	return &Service{
		entries:  map[string]*Entry{},
		bindings: map[string]map[string]struct{}{},
	}
}

// Create 新建。
func (s *Service) Create(_ context.Context, title, summary, content string, tags []string) (*Entry, error) {
	if title == "" {
		return nil, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	e := &Entry{
		ID: idgen.New("KNW"), Title: title, Summary: summary, Content: content, Tags: tags,
		CreatedAt: now, UpdatedAt: now,
	}
	s.entries[e.ID] = e
	cp := *e
	if tags != nil {
		cp.Tags = append([]string{}, tags...)
	}
	return &cp, nil
}

// Update 更新。
func (s *Service) Update(_ context.Context, id, title, summary, content string, tags []string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	if title != "" {
		e.Title = title
	}
	if summary != "" {
		e.Summary = summary
	}
	if content != "" {
		e.Content = content
	}
	if tags != nil {
		e.Tags = append([]string{}, tags...)
	}
	e.UpdatedAt = time.Now().UTC()
	cp := *e
	cp.Tags = append([]string{}, e.Tags...)
	return &cp, nil
}

// Get 详情。
func (s *Service) Get(_ context.Context, id string) (*Entry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *e
	cp.Tags = append([]string{}, e.Tags...)
	return &cp, nil
}

// List 全部。
func (s *Service) List(_ context.Context) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Entry, 0, len(s.entries))
	for _, e := range s.entries {
		cp := *e
		cp.Tags = append([]string{}, e.Tags...)
		out = append(out, &cp)
	}
	return out
}

// Delete 删除并解绑。
func (s *Service) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[id]; !ok {
		return ErrNotFound
	}
	delete(s.entries, id)
	for emp, set := range s.bindings {
		delete(set, id)
		if len(set) == 0 {
			delete(s.bindings, emp)
		}
	}
	return nil
}

// Bind 绑定。
func (s *Service) Bind(_ context.Context, employeeID, knowledgeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[knowledgeID]; !ok {
		return ErrNotFound
	}
	if s.bindings[employeeID] == nil {
		s.bindings[employeeID] = map[string]struct{}{}
	}
	s.bindings[employeeID][knowledgeID] = struct{}{}
	return nil
}

// Unbind 解绑。
func (s *Service) Unbind(_ context.Context, employeeID, knowledgeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if set := s.bindings[employeeID]; set != nil {
		delete(set, knowledgeID)
	}
	return nil
}

// ListByEmployee Employee 已绑知识。
func (s *Service) ListByEmployee(_ context.Context, employeeID string) []*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.bindings[employeeID]
	out := make([]*Entry, 0, len(set))
	for id := range set {
		if e, ok := s.entries[id]; ok {
			cp := *e
			cp.Tags = append([]string{}, e.Tags...)
			out = append(out, &cp)
		}
	}
	return out
}
