// Package cursor 实现 Cursor Provider / Installer（V1：本机 Detect Cursor CLI `agent`）。
// 设计依据：设计文档 §36–§38、§98、§118、§119。
// 官方 ACP：`agent acp`（stdio JSON-RPC）。禁止启动 Cursor.app GUI。
package cursor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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

const fakeAgentBinary = "agent-fake"

// Provider Cursor AgentProvider。
type Provider struct {
	BinaryOverride string
	ACP            acp.Client // 测试可注入 Fake；生产 Start 时对真实 agent 走 Stdio
	Proc           *process.Manager
	mu             sync.Mutex
	sessions       map[string]*sessionRec
	state          string
}

type sessionRec struct {
	acpSess acp.Session
	status  providers.SessionStatus
}

// NewProvider 创建；未注入 ACP 时用 Fake（仅测试/无 agent 时）。
func NewProvider(proc *process.Manager, binary string) *Provider {
	if proc == nil {
		proc = process.NewManager("agent", "agent.exe", fakeAgentBinary)
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

// Detect 探测本机 Cursor CLI `agent`（ACP Server），不探测 GUI Cursor.app。
func (p *Provider) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	path := p.BinaryOverride
	if path == "" {
		path = lookupAgent()
	}
	if path == "" {
		p.state = StateNotInstalled
		return &providers.InstallInfo{Name: "cursor", Path: "", Version: ""}, nil
	}
	p.state = StateInstalled
	p.Proc.Allow(path)
	return &providers.InstallInfo{Name: "cursor", Path: path, Version: "agent-cli"}, nil
}

func findUserHome() string {
	home, _ := os.UserHomeDir()
	if !strings.Contains(strings.ToLower(home), "systemprofile") && home != "" {
		return home
	}
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	usersDir := filepath.Join(sysDrive, "\\Users")
	entries, err := os.ReadDir(usersDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "Public" || name == "Default" || name == "All Users" || name == "Default User" {
			continue
		}
		candidate := filepath.Join(usersDir, name)
		if _, err := os.Stat(filepath.Join(candidate, ".cursor", "cli-config.json")); err == nil {
			return candidate
		}
		if _, err := os.Stat(filepath.Join(candidate, ".aie")); err == nil {
			return candidate
		}
	}
	return ""
}

// lookupAgent 查找 Cursor CLI `agent` 可执行文件。
// 官方文档：默认路径 ~/.local/bin/agent；命令为 `agent acp`。
func lookupAgent() string {
	if p, err := exec.LookPath("agent"); err == nil {
		return p
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "agent"),
		filepath.Join(home, ".cursor", "bin", "agent"),
	}
	switch runtime.GOOS {
	case "windows":
		candidates = append(candidates,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "cursor-agent", "agent.cmd"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "cursor-agent", "cursor-agent.cmd"),
			filepath.Join(home, ".local", "bin", "agent.cmd"),
			filepath.Join(home, ".local", "bin", "agent.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "cursor-agent", "agent.exe"),
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
				if u == "Public" || u == "Default" || u == "All Users" {
					continue
				}
				candidates = append(candidates,
					filepath.Join(sysDrive, "\\Users", u, "AppData", "Local", "cursor-agent", "agent.cmd"),
					filepath.Join(sysDrive, "\\Users", u, "AppData", "Local", "cursor-agent", "cursor-agent.cmd"),
					filepath.Join(sysDrive, "\\Users", u, ".local", "bin", "agent.cmd"),
					filepath.Join(sysDrive, "\\Users", u, ".local", "bin", "agent.exe"),
				)
			}
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// isGUICursorBinary 识别 Cursor.app / Cursor.exe GUI，禁止当作 ACP Server。
func isGUICursorBinary(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "Cursor", "Cursor.exe", "cursor":
		// /Applications/Cursor.app/.../Cursor 或 Cursor.exe
		return true
	}
	return false
}

// Start 启动会话：spawn `agent acp` → ACP Handshake → READY。绝不打开 IDE GUI。
func (p *Provider) Start(ctx context.Context, spec providers.StartSpec) (providers.AgentSession, error) {
	info, err := p.Detect(ctx)
	if err != nil {
		return nil, err
	}
	useFake := false
	switch {
	case info.Path == "":
		useFake = true
		info.Path = fakeAgentBinary
		p.Proc.Allow(fakeAgentBinary)
	case info.Path == fakeAgentBinary || filepath.Base(info.Path) == "cursor-fake":
		// 测试 / 开发 Fake 模式
		useFake = true
		p.Proc.Allow(info.Path)
	case isGUICursorBinary(info.Path):
		return nil, errors.New("检测到 Cursor GUI 可执行文件；ACP 必须使用 Cursor CLI `agent acp`，请安装 agent CLI 或配置 providers.cursor.path 指向 agent")
	}

	p.mu.Lock()
	p.state = StateStarting
	p.mu.Unlock()

	sid := spec.SessionID
	if sid == "" {
		sid = "ses-local"
	}

	var acpSess acp.Session
	if useFake {
		acpSess = p.ACP.Open(sid)
	} else {
		stdioSess := acp.NewStdioSession(sid, info.Path, []string{"acp"}, spec.WorkspacePath)
		if uHome := findUserHome(); uHome != "" {
			stdioSess.SetEnv([]string{
				"USERPROFILE=" + uHome,
				"HOME=" + uHome,
				"APPDATA=" + filepath.Join(uHome, "AppData", "Roaming"),
				"LOCALAPPDATA=" + filepath.Join(uHome, "AppData", "Local"),
			})
		}
		if len(spec.MCPServers) > 0 {
			stdioSess.SetMCPServers(spec.MCPServers)
		}
		if spec.Model != "" {
			stdioSess.SetModel(spec.Model)
		}
		acpSess = stdioSess
	}
	if err := acpSess.Start(ctx); err != nil {
		p.mu.Lock()
		p.state = StateError
		p.mu.Unlock()
		return nil, err
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
func (a *agentSession) Send(ctx context.Context, input []byte) (string, error) {
	a.p.mu.Lock()
	a.p.state = StateBusy
	a.p.mu.Unlock()
	return a.acp.Send(ctx, input)
}

// LastUsage 读取本次 ACP prompt 的 token 用量。
func (a *agentSession) LastUsage() (int64, int64, string, string) {
	type reporter interface {
		LastUsage() acp.Usage
	}
	if r, ok := a.acp.(reporter); ok {
		u := r.LastUsage()
		if u.Agent == "" {
			u.Agent = "cursor"
		}
		return u.InputTokens, u.OutputTokens, u.Agent, u.Source
	}
	return 0, 0, "cursor", ""
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
	return errors.New("V1 不支持 Registry 下载安装；请本机安装 Cursor CLI `agent`（agent acp），或配置 providers.cursor.path")
}

func (i *Installer) Update(context.Context, providers.UpdateSpec) error {
	return i.Install(context.Background(), providers.InstallSpec{})
}

func (i *Installer) Uninstall(context.Context) error {
	return errors.New("V1 不支持自动卸载")
}
