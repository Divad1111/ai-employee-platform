// Package codex 实现 Codex Provider / Installer（V1：本机 Detect）。
// 与 Cursor 同等接口；Job 选择由 Employee 配置决定。
// 设计依据：设计文档 §36、§98。
package codex

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

const (
	StateNotInstalled = "NOT_INSTALLED"
	StateInstalled    = "INSTALLED"
	StateReady        = "READY"
	StateBusy         = "BUSY"
	StateError        = "ERROR"
)

// Provider Codex。
type Provider struct {
	BinaryOverride string
	ACP            acp.Client
	Proc           *process.Manager
	mu             sync.Mutex
	sessions       map[string]acp.Session
	state          string
}

func NewProvider(proc *process.Manager, binary string) *Provider {
	if proc == nil {
		proc = process.NewManager("codex", "codex.exe", "codex")
	}
	return &Provider{
		BinaryOverride: binary,
		ACP:            &acp.FakeClient{},
		Proc:           proc,
		sessions:       map[string]acp.Session{},
		state:          StateNotInstalled,
	}
}

func (p *Provider) Name() string { return "codex" }

func (p *Provider) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	path := p.BinaryOverride
	if path == "" {
		path = lookupCodex()
	}
	if path == "" {
		p.state = StateNotInstalled
		return &providers.InstallInfo{Name: "codex"}, nil
	}
	p.state = StateInstalled
	p.Proc.Allow(path)
	return &providers.InstallInfo{Name: "codex", Path: path, Version: "detected"}, nil
}

func lookupCodex() string {
	if p, err := exec.LookPath("codex"); err == nil {
		return p
	}
	switch runtime.GOOS {
	case "windows":
		c := filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "codex", "codex.exe")
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	case "darwin":
		c := "/usr/local/bin/codex"
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func (p *Provider) Start(ctx context.Context, spec providers.StartSpec) (providers.AgentSession, error) {
	info, _ := p.Detect(ctx)
	if info.Path == "" {
		info.Path = "codex-fake"
		p.Proc.Allow("codex-fake")
	}
	sid := spec.SessionID
	if sid == "" {
		sid = "ses-codex"
	}
	s := p.ACP.Open(sid)
	if err := s.Start(ctx); err != nil {
		p.state = StateError
		return nil, err
	}
	p.mu.Lock()
	p.sessions[sid] = s
	p.state = StateReady
	p.mu.Unlock()
	return &agentSession{p: p, id: sid, acp: s}, nil
}

func (p *Provider) Stop(ctx context.Context, sessionID string) error {
	p.mu.Lock()
	s, ok := p.sessions[sessionID]
	delete(p.sessions, sessionID)
	p.mu.Unlock()
	if !ok {
		return errors.New("session 不存在")
	}
	return s.Stop(ctx)
}

func (p *Provider) Status(_ context.Context, sessionID string) (*providers.SessionStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.sessions[sessionID]; !ok {
		return &providers.SessionStatus{SessionID: sessionID, State: p.state}, nil
	}
	return &providers.SessionStatus{SessionID: sessionID, State: StateReady}, nil
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

// Installer V1 Detect-only。
type Installer struct{ Path string }

func (i *Installer) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	return NewProvider(nil, i.Path).Detect(ctx)
}
func (i *Installer) Install(context.Context, providers.InstallSpec) error {
	return errors.New("V1 不支持 Registry 下载；请本机安装或配置 providers.codex.path")
}
func (i *Installer) Update(context.Context, providers.UpdateSpec) error {
	return i.Install(context.Background(), providers.InstallSpec{})
}
func (i *Installer) Uninstall(context.Context) error {
	return errors.New("V1 不支持自动卸载")
}
