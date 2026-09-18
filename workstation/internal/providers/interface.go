// Package providers 定义 Agent Provider 与 Installer 抽象。
// Job / Session 只依赖接口，禁止写死 Cursor。
// 设计依据：设计文档 §36、§37。
package providers

import "context"

// InstallInfo 描述本机已安装的 Provider 信息。
type InstallInfo struct {
	// Name 提供方名称，如 cursor / codex
	Name string
	// Version 版本号
	Version string
	// Path 可执行文件或安装根路径
	Path string
}

// InstallSpec 描述安装请求（完整下载安装在 V2 Registry）。
type InstallSpec struct {
	// Version 目标版本；空表示最新或本机探测
	Version string
}

// UpdateSpec 描述更新请求。
type UpdateSpec struct {
	// Version 目标版本
	Version string
}

// StartSpec 描述启动 Agent Session 的参数。
type StartSpec struct {
	// EmployeeID 员工 ID
	EmployeeID string
	// WorkspacePath 工作区绝对路径
	WorkspacePath string
	// SessionID 会话 ID（可由上层生成）
	SessionID string
}

// SessionStatus 描述会话运行状态摘要。
type SessionStatus struct {
	SessionID string
	State     string
	PID       int
}

// AgentSession 表示一次可交互的 Agent 会话（经 ACP）。
// 设计依据：§117。
type AgentSession interface {
	// Start 启动会话并完成握手。
	Start(ctx context.Context) error
	// Send 向 Agent 发送输入。
	Send(ctx context.Context, input []byte) error
	// Stop 优雅停止会话。
	Stop(ctx context.Context) error
}

// AgentProvider 负责已安装 Provider 的启停与状态。
type AgentProvider interface {
	// Name 返回提供方名称。
	Name() string
	// Detect 探测本机安装情况。
	Detect(ctx context.Context) (*InstallInfo, error)
	// Start 启动会话。
	Start(ctx context.Context, spec StartSpec) (AgentSession, error)
	// Stop 停止指定会话。
	Stop(ctx context.Context, sessionID string) error
	// Status 查询会话状态。
	Status(ctx context.Context, sessionID string) (*SessionStatus, error)
}

// ProviderInstaller 负责安装/更新/卸载，与运行时 Provider 分离。
type ProviderInstaller interface {
	// Detect 探测安装状态。
	Detect(ctx context.Context) (*InstallInfo, error)
	// Install 执行安装。
	Install(ctx context.Context, spec InstallSpec) error
	// Update 执行更新。
	Update(ctx context.Context, spec UpdateSpec) error
	// Uninstall 卸载。
	Uninstall(ctx context.Context) error
}
