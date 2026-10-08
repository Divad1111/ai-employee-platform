package tokenusage

import (
	"fmt"
	"sync"
)

// Registry Provider 注册表。
type Registry struct {
	mu   sync.RWMutex
	byName map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]Provider{}}
}

func (r *Registry) Register(p Provider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[p.Name()] = p
}

func (r *Registry) Get(runtime AgentRuntime) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, p := range r.byName {
		if p.Supports(runtime) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrProviderNotSupported, runtime)
}
