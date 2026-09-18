// Package cursor 实现 Cursor Provider / Installer（V1：本机 Detect）。
// 设计依据：设计文档 §36–§38、§98、§118、§119。
package cursor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/ai-employee-platform/workstation/internal/acp"
	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

// 生命周期状态 §119。
const (
	StateNotInstalled = "NOT_INSTALLED"
	StateInstalled    = "INSTALLED"
	StateStarting     = "STARTING"
	StateReady        = "READY"
	StateBusy         = "BUSY"
	StateStopping     = "STOPPING"
	StateError        = "ERROR"
)

// Provider Cursor AgentProvider。
type Provider struct {
	BinaryOverride string
	ACP            acp.Client
	Proc           *process.Manager
	mu             sync.Mutex
	sessions       map[string]*sessionRec
	state          string
}

type sessionRec struct {
	acpSess acp.Session
	status  providers.SessionStatus
}

// NewProvider 创建；未注入 ACP 时用 Fake。
func NewProvider(proc *process.Manager, binary string) *Provider {
	if proc == nil {
		proc = process.NewManager("cursor", "cursor.exe", "Cursor")
	}
	p := &Provider{
		BinaryOverride: binary,
		ACP:            &acp.FakeClient{},
		Proc:           proc,
		sessions:       map[string]*sessionRec{},
		state:          StateNotInstalled,
	}
	return p
}

func (p *Provider) Name() string { return "cursor" }

// Detect 探测本机安装。
func (p *Provider) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	path := p.BinaryOverride
	if path == "" {
		path = lookupCursor()
	}
	if path == "" {
		p.state = StateNotInstalled
		return &providers.InstallInfo{Name: "cursor", Path: "", Version: ""}, nil
	}
	p.state = StateInstalled
	p.Proc.Allow(path)
	return &providers.InstallInfo{Name: "cursor", Path: path, Version: "detected"}, nil
}

func lookupCursor() string {
	candidates := []string{}
	switch runtime.GOOS {
	case "windows":
		candidates = append(candidates,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "cursor", "Cursor.exe"),
			filepath.Join(os.Getenv("ProgramFiles"), "Cursor", "Cursor.exe"),
		)
	case "darwin":
		candidates = append(candidates, "/Applications/Cursor.app/Contents/MacOS/Cursor")
	default:
		candidates = append(candidates, "/usr/bin/cursor", "/usr/local/bin/cursor")
	}
	if p, err := exec.LookPath("cursor"); err == nil {
		return p
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// Start 启动会话并 ACP Handshake → READY。
func (p *Provider) Start(ctx context.Context, spec providers.StartSpec) (providers.AgentSession, error) {
	info, err := p.Detect(ctx)
	if err != nil {
		return nil, err
	}
	if info.Path == "" && p.BinaryOverride == "" {
		// V1 允许 Fake 模式：无本机安装时仍可走 ACP Fake（测试/开发）
		info.Path = "cursor-fake"
		p.Proc.Allow("cursor-fake")
	}
	p.mu.Lock()
	p.state = StateStarting
	p.mu.Unlock()

	sid := spec.SessionID
	if sid == "" {
		sid = "ses-local"
	}
	acpSess := p.ACP.Open(sid)
	if err := acpSess.Start(ctx); err != nil {
		p.mu.Lock()
		p.state = StateError
		p.mu.Unlock()
		return nil, err
	}
	// 非 fake 路径才真正拉进程
	if info.Path != "cursor-fake" {
		_, _ = p.Proc.Start(ctx, process.Spec{
			ID: sid, Name: "cursor", Binary: info.Path,
			Args: []string{"--acp"}, WorkDir: spec.WorkspacePath,
		})
	}
	rec := &sessionRec{
		acpSess: acpSess,
		status:  providers.SessionStatus{SessionID: sid, State: StateReady, PID: 0},
	}
	p.mu.Lock()
	p.sessions[sid] = rec
	p.state = StateReady
	p.mu.Unlock()
	return &agentSession{p: p, id: sid, acp: acpSess}, nil
}

func (p *Provider) Stop(ctx context.Context, sessionID string) error {
	p.mu.Lock()
	rec, ok := p.sessions[sessionID]
	if ok {
		delete(p.sessions, sessionID)
	}
	p.state = StateStopping
	p.mu.Unlock()
	if !ok {
		return errors.New("session 不存在")
	}
	_ = rec.acpSess.Stop(ctx)
	_ = p.Proc.Stop(ctx, sessionID)
	p.mu.Lock()
	p.state = StateInstalled
	p.mu.Unlock()
	return nil
}

func (p *Provider) Status(_ context.Context, sessionID string) (*providers.SessionStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.sessions[sessionID]
	if !ok {
		return &providers.SessionStatus{SessionID: sessionID, State: StateInstalled}, nil
	}
	cp := rec.status
	return &cp, nil
}

// State 当前 Provider 生命周期。
func (p *Provider) State() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

type agentSession struct {
	p   *Provider
	id  string
	acp acp.Session
}

func (a *agentSession) Start(ctx context.Context) error { return a.acp.Start(ctx) }
func (a *agentSession) Send(ctx context.Context, input []byte) error {
	a.p.mu.Lock()
	a.p.state = StateBusy
	a.p.mu.Unlock()
	return a.acp.Send(ctx, input)
}
func (a *agentSession) Stop(ctx context.Context) error { return a.p.Stop(ctx, a.id) }

// Installer V1：仅 Detect；Install 返回明确未实现完整 Registry。
type Installer struct {
	Path string
}

func (i *Installer) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	return NewProvider(nil, i.Path).Detect(ctx)
}

func (i *Installer) Install(context.Context, providers.InstallSpec) error {
	return errors.New("V1 不支持 Registry 下载安装；请本机安装 Cursor 或配置 providers.cursor.path")
}

func (i *Installer) Update(context.Context, providers.UpdateSpec) error {
	return i.Install(context.Background(), providers.InstallSpec{})
}

func (i *Installer) Uninstall(context.Context) error {
	return errors.New("V1 不支持自动卸载")
}
