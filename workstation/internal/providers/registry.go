package providers

import (
	"fmt"
	"sync"
)

// Registry 按名称查找 Provider（JobManager 不依赖具体 Cursor 类型）。
type Registry struct {
	mu   sync.RWMutex
	by   map[string]AgentProvider
}

// NewRegistry 创建。
func NewRegistry() *Registry {
	return &Registry{by: map[string]AgentProvider{}}
}

// Register 注册。
func (r *Registry) Register(p AgentProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.by[p.Name()] = p
}

// Get 获取。
func (r *Registry) Get(name string) (AgentProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.by[name]
	if !ok {
		return nil, fmt.Errorf("未知 Provider: %s", name)
	}
	return p, nil
}

// List 名称列表。
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.by))
	for k := range r.by {
		out = append(out, k)
	}
	return out
}
