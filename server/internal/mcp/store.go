package mcp

import (
	"context"
	"errors"
)

var (
	ErrServerNotFound     = errors.New("MCP 服务不存在")
	ErrServerNameConflict = errors.New("MCP 服务名称已存在")
	ErrCannotDeleteBuiltin = errors.New("系统内置 MCP 服务禁止删除")
	ErrCredentialNotFound = errors.New("凭证不存在")
	ErrBindingNotFound    = errors.New("数字员工 MCP 绑定不存在")
	ErrBindingConflict    = errors.New("该数字员工已绑定该 MCP 服务")
)

// Store 定义 MCP 相关的持久化接口。
type Store interface {
	// MCP Servers
	ListServers(ctx context.Context) ([]*MCPServer, error)
	GetServer(ctx context.Context, id string) (*MCPServer, error)
	CreateServer(ctx context.Context, s *MCPServer) error
	UpdateServer(ctx context.Context, s *MCPServer) error
	DeleteServer(ctx context.Context, id string) error

	// Credentials
	ListCredentials(ctx context.Context, ownerType, ownerID string) ([]*Credential, error)
	GetCredential(ctx context.Context, id string) (*Credential, error)
	CreateCredential(ctx context.Context, c *Credential) error
	UpdateCredential(ctx context.Context, c *Credential) error
	DeleteCredential(ctx context.Context, id string) error

	// Employee MCP Bindings
	ListBindingsByEmployee(ctx context.Context, employeeID string) ([]*EmployeeMCPBinding, error)
	ListBindingsByServer(ctx context.Context, serverID string) ([]*EmployeeMCPBinding, error)
	GetBinding(ctx context.Context, id string) (*EmployeeMCPBinding, error)
	GetBindingByEmployeeAndServer(ctx context.Context, employeeID, serverID string) (*EmployeeMCPBinding, error)
	CreateBinding(ctx context.Context, b *EmployeeMCPBinding) error
	UpdateBinding(ctx context.Context, b *EmployeeMCPBinding) error
	DeleteBinding(ctx context.Context, id string) error
}
