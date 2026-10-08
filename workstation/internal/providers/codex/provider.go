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
	"github.com/ai-employee-platform/workstation/internal/tokenusage"
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
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "codex"),
		"/opt/homebrew/bin/codex",
		"/usr/local/bin/codex",
		"/Applications/ChatGPT.app/Contents/Resources/codex",
		filepath.Join(home, ".codex", "plugins", ".plugin-appserver", "codex"),
	}
	switch runtime.GOOS {
	case "windows":
		candidates = append(candidates,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "OpenAI", "Codex", "bin", "codex.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "codex", "codex.exe"),
		)
		sysDrive := os.Getenv("SystemDrive")
		if sysDrive == "" {
			sysDrive = "C:"
		}
		if entries, err := os.ReadDir(filepath.Join(sysDrive, "\\Users")); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				u := entry.Name()
				if u == "Public" || u == "Default" || u == "All Users" || u == "Default User" {
					continue
				}
				candidates = append(candidates,
					filepath.Join(sysDrive, "\\Users", u, "AppData", "Local", "Programs", "OpenAI", "Codex", "bin", "codex.exe"),
					filepath.Join(sysDrive, "\\Users", u, "AppData", "Local", "Programs", "codex", "codex.exe"),
					filepath.Join(sysDrive, "\\Users", u, ".local", "bin", "codex.exe"),
					filepath.Join(sysDrive, "\\Users", u, ".local", "bin", "codex.cmd"),
				)
			}
		}
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func (p *Provider) Start(ctx context.Context, spec providers.StartSpec) (providers.AgentSession, error) {
	info, err := p.Detect(ctx)
	if err != nil {
		return nil, err
	}
	useFake := info.Path == "" || info.Path == "codex-fake" || filepath.Base(info.Path) == "codex-fake"
	if info.Path == "" {
		return nil, errors.New("未检测到 Codex CLI：请安装 Codex 桌面应用或 `codex`，或在 providers.codex.path 配置路径")
	}
	if useFake {
		p.Proc.Allow(info.Path)
	} else {
		p.Proc.Allow(info.Path)
	}

	sid := spec.SessionID
	if sid == "" {
		sid = "ses-codex"
	}
	var acpSess acp.Session
	if useFake {
		acpSess = p.ACP.Open(sid)
	} else {
		stdioSess := newAppServerSession(sid, info.Path, spec.WorkspacePath, spec.Model)
		if len(spec.MCPServers) > 0 {
			stdioSess.SetMCPServers(spec.MCPServers)
		}
		acpSess = stdioSess
	}
	if err := acpSess.Start(ctx); err != nil {
		p.mu.Lock()
		p.state = StateError
		p.mu.Unlock()
		return nil, err
	}
	p.mu.Lock()
	p.sessions[sid] = acpSess
	p.state = StateReady
	p.mu.Unlock()
	return &agentSession{p: p, id: sid, acp: acpSess}, nil
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
func (a *agentSession) Send(ctx context.Context, input []byte) (string, error) {
	a.p.mu.Lock()
	a.p.state = StateBusy
	a.p.mu.Unlock()
	return a.acp.Send(ctx, input)
}

// LastUsage 兼容旧接口。
func (a *agentSession) LastUsage() (int64, int64, string, string) {
	if u := a.LastTokenUsage(); u != nil {
		return u.InputTokens, u.OutputTokens, firstNonEmpty(u.Provider, "codex"), u.Source
	}
	return 0, 0, "codex", "unavailable"
}

// LastTokenUsage 读取 Codex 真实用量。
func (a *agentSession) LastTokenUsage() *tokenusage.TokenUsage {
	type reporter interface {
		LastTokenUsage() *tokenusage.TokenUsage
	}
	if r, ok := a.acp.(reporter); ok {
		return r.LastTokenUsage()
	}
	type legacy interface {
		LastUsage() acp.Usage
	}
	if r, ok := a.acp.(legacy); ok {
		u := r.LastUsage()
		agent := u.Agent
		if agent == "" || agent == "cursor" {
			agent = "codex"
		}
		out := &tokenusage.TokenUsage{
			InputTokens:  u.InputTokens,
			OutputTokens: u.OutputTokens,
			Provider:     agent,
			Source:       firstNonEmpty(u.Source, "codex_cli_event"),
		}
		out.Normalize()
		if !out.HasAny() {
			out.UsageStatus = tokenusage.StatusUnavailable
			out.Source = "unavailable"
		}
		return out
	}
	return &tokenusage.TokenUsage{Provider: "codex", Source: "unavailable", UsageStatus: tokenusage.StatusUnavailable}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
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
