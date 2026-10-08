package tokenusage

import "context"

// Provider 从具体 Agent Runtime 读取真实 usage。
type Provider interface {
	Name() string
	Supports(runtime AgentRuntime) bool
	GetRunUsage(ctx context.Context, execution *AgentExecution, session any) (*TokenUsage, error)
}

// SessionReporter 会话可选实现：返回最近一次 Send 的真实 usage。
type SessionReporter interface {
	LastTokenUsage() *TokenUsage
}
