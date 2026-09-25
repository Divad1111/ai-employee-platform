package mcp

import (
	"context"
	"sync"
)

type memoryStore struct {
	mu          sync.RWMutex
	servers     map[string]*MCPServer
	credentials map[string]*Credential
	bindings    map[string]*EmployeeMCPBinding
}

// NewMemoryStore 返回内存实现的 MCP Store（测试与轻量模式）。
func NewMemoryStore() Store {
	return &memoryStore{
		servers: map[string]*MCPServer{
			"mcp-workflow": {
				ID: "mcp-workflow", Name: "workflow-mcp",
				Description: "系统内置工作流、技能包与企业知识库 MCP 服务",
				ServerType: ServerTypeBuiltin, Transport: TransportHTTP,
				Endpoint: "/mcp", Status: StatusActive,
			},
		},
		credentials: map[string]*Credential{},
		bindings:    map[string]*EmployeeMCPBinding{},
	}
}

func (m *memoryStore) ListServers(_ context.Context) ([]*MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*MCPServer
	for _, s := range m.servers {
		cp := *s
		// 计算绑定数
		count := 0
		for _, b := range m.bindings {
			if b.MCPServerID == s.ID && b.Enabled {
				count++
			}
		}
		cp.BoundEmployeeCount = count
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memoryStore) GetServer(_ context.Context, id string) (*MCPServer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.servers[id]
	if !ok {
		return nil, ErrServerNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *memoryStore) CreateServer(_ context.Context, s *MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.servers[s.ID] = s
	return nil
}

func (m *memoryStore) UpdateServer(_ context.Context, s *MCPServer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.servers[s.ID]; !ok {
		return ErrServerNotFound
	}
	m.servers[s.ID] = s
	return nil
}

func (m *memoryStore) DeleteServer(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok {
		return ErrServerNotFound
	}
	if s.ServerType == ServerTypeBuiltin {
		return ErrCannotDeleteBuiltin
	}
	delete(m.servers, id)
	return nil
}

func (m *memoryStore) ListCredentials(_ context.Context, ownerType, ownerID string) ([]*Credential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Credential
	for _, c := range m.credentials {
		if ownerType != "" && c.OwnerType != ownerType {
			continue
		}
		if ownerID != "" && c.OwnerID != ownerID {
			continue
		}
		cp := *c
		count := 0
		for _, b := range m.bindings {
			if b.CredentialID == c.ID {
				count++
			}
		}
		cp.BoundEmployeeCount = count
		out = append(out, &cp)
	}
	return out, nil
}

func (m *memoryStore) GetCredential(_ context.Context, id string) (*Credential, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.credentials[id]
	if !ok {
		return nil, ErrCredentialNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *memoryStore) CreateCredential(_ context.Context, c *Credential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.credentials[c.ID] = c
	return nil
}

func (m *memoryStore) UpdateCredential(_ context.Context, c *Credential) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.credentials[c.ID]; !ok {
		return ErrCredentialNotFound
	}
	m.credentials[c.ID] = c
	return nil
}

func (m *memoryStore) DeleteCredential(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.credentials[id]; !ok {
		return ErrCredentialNotFound
	}
	delete(m.credentials, id)
	return nil
}

func (m *memoryStore) ListBindingsByEmployee(_ context.Context, employeeID string) ([]*EmployeeMCPBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*EmployeeMCPBinding
	for _, b := range m.bindings {
		if b.EmployeeID == employeeID {
			cp := *b
			if s, ok := m.servers[b.MCPServerID]; ok {
				cp.MCPServerName = s.Name
				cp.MCPServerType = s.ServerType
				cp.MCPTransport = s.Transport
			}
			if c, ok := m.credentials[b.CredentialID]; ok {
				cp.CredentialName = c.CredentialName
				cp.CredentialProvider = c.Provider
				cp.CredentialMasked = c.MaskedValue
			}
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memoryStore) ListBindingsByServer(_ context.Context, serverID string) ([]*EmployeeMCPBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*EmployeeMCPBinding
	for _, b := range m.bindings {
		if b.MCPServerID == serverID {
			cp := *b
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memoryStore) GetBinding(_ context.Context, id string) (*EmployeeMCPBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.bindings[id]
	if !ok {
		return nil, ErrBindingNotFound
	}
	cp := *b
	return &cp, nil
}

func (m *memoryStore) GetBindingByEmployeeAndServer(_ context.Context, employeeID, serverID string) (*EmployeeMCPBinding, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, b := range m.bindings {
		if b.EmployeeID == employeeID && b.MCPServerID == serverID {
			cp := *b
			return &cp, nil
		}
	}
	return nil, ErrBindingNotFound
}

func (m *memoryStore) CreateBinding(_ context.Context, b *EmployeeMCPBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bindings[b.ID] = b
	return nil
}

func (m *memoryStore) UpdateBinding(_ context.Context, b *EmployeeMCPBinding) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bindings[b.ID]; !ok {
		return ErrBindingNotFound
	}
	m.bindings[b.ID] = b
	return nil
}

func (m *memoryStore) DeleteBinding(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.bindings[id]; !ok {
		return ErrBindingNotFound
	}
	delete(m.bindings, id)
	return nil
}
