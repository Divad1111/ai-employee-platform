// Package skill Employee 技能定义与绑定。
// 设计依据：设计文档 §8、§32。
package skill

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/idgen"
)

// 错误。
var (
	ErrNotFound     = errors.New("skill 不存在")
	ErrInvalidInput = errors.New("参数无效")
)

// Skill 技能目录项。
type Skill struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Binding Employee ↔ Skill。
type Binding struct {
	EmployeeID string `json:"employee_id"`
	SkillID    string `json:"skill_id"`
}

// Service 内存技能服务。
type Service struct {
	mu       sync.RWMutex
	skills   map[string]*Skill
	bindings map[string]map[string]struct{} // employee → skill set
}

// NewService 创建。
func NewService() *Service {
	return &Service{
		skills:   map[string]*Skill{},
		bindings: map[string]map[string]struct{}{},
	}
}

// Create 新建技能。
func (s *Service) Create(_ context.Context, name, desc, category string) (*Skill, error) {
	if name == "" {
		return nil, ErrInvalidInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	sk := &Skill{
		ID: idgen.New("SKL"), Name: name, Description: desc, Category: category,
		CreatedAt: now, UpdatedAt: now,
	}
	s.skills[sk.ID] = sk
	cp := *sk
	return &cp, nil
}

// Update 更新。
func (s *Service) Update(_ context.Context, id, name, desc, category string) (*Skill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sk, ok := s.skills[id]
	if !ok {
		return nil, ErrNotFound
	}
	if name != "" {
		sk.Name = name
	}
	if desc != "" {
		sk.Description = desc
	}
	if category != "" {
		sk.Category = category
	}
	sk.UpdatedAt = time.Now().UTC()
	cp := *sk
	return &cp, nil
}

// Get 详情。
func (s *Service) Get(_ context.Context, id string) (*Skill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sk, ok := s.skills[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *sk
	return &cp, nil
}

// List 全部。
func (s *Service) List(_ context.Context) []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Skill, 0, len(s.skills))
	for _, sk := range s.skills {
		cp := *sk
		out = append(out, &cp)
	}
	return out
}

// Delete 删除并解绑。
func (s *Service) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.skills[id]; !ok {
		return ErrNotFound
	}
	delete(s.skills, id)
	for emp, set := range s.bindings {
		delete(set, id)
		if len(set) == 0 {
			delete(s.bindings, emp)
		}
	}
	return nil
}

// Bind 绑定到 Employee。
func (s *Service) Bind(_ context.Context, employeeID, skillID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.skills[skillID]; !ok {
		return ErrNotFound
	}
	if s.bindings[employeeID] == nil {
		s.bindings[employeeID] = map[string]struct{}{}
	}
	s.bindings[employeeID][skillID] = struct{}{}
	return nil
}

// Unbind 解绑。
func (s *Service) Unbind(_ context.Context, employeeID, skillID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if set := s.bindings[employeeID]; set != nil {
		delete(set, skillID)
	}
	return nil
}

// ListByEmployee Employee 已绑技能。
func (s *Service) ListByEmployee(_ context.Context, employeeID string) []*Skill {
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := s.bindings[employeeID]
	out := make([]*Skill, 0, len(set))
	for id := range set {
		if sk, ok := s.skills[id]; ok {
			cp := *sk
			out = append(out, &cp)
		}
	}
	return out
}
