package antigravity

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

// 生命周期状态。
const (
	StateNotInstalled = "NOT_INSTALLED"
	StateInstalled    = "INSTALLED"
	StateStarting     = "STARTING"
	StateReady        = "READY"
	StateBusy         = "BUSY"
	StateStopping     = "STOPPING"
	StateError        = "ERROR"
)

const fakeAgentBinary = "antigravity-fake"

// Provider Antigravity AgentProvider。
type Provider struct {
	BinaryOverride string
	ACP            acp.Client // 测试可注入 FakeClient；生产启动真实 agent 时走 RealSession
	Proc           *process.Manager
	mu             sync.Mutex
	sessions       map[string]providers.AgentSession
	state          string
}

// NewProvider 创建 Antigravity Provider。
func NewProvider(proc *process.Manager, binary string) *Provider {
	if proc == nil {
		proc = process.NewManager(
			"antigravity", "antigravity.exe", "antigravity.cmd",
			"agy", "agy.exe", "agy.cmd",
			"language_server", "language_server.exe",
			"agentapi.bat", fakeAgentBinary,
		)
	}
	return &Provider{
		BinaryOverride: binary,
		ACP:            &acp.FakeClient{},
		Proc:           proc,
		sessions:       map[string]providers.AgentSession{},
		state:          StateNotInstalled,
	}
}

func (p *Provider) Name() string { return "antigravity" }

// Detect 探测本机 Antigravity 安装。
func (p *Provider) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	path := p.BinaryOverride
	if path == "" {
		path = lookupAntigravity()
	}
	if path == "" {
		p.mu.Lock()
		p.state = StateNotInstalled
		p.mu.Unlock()
		return &providers.InstallInfo{Name: "antigravity", Path: "", Version: ""}, nil
	}
	p.mu.Lock()
	p.state = StateInstalled
	p.mu.Unlock()
	p.Proc.Allow(path)
	return &providers.InstallInfo{Name: "antigravity", Path: path, Version: "detected"}, nil
}

// lookupAntigravity 查找本机 Antigravity 可执行文件或 agentapi。
func lookupAntigravity() string {
	for _, bin := range []string{"agy", "antigravity", "language_server"} {
		if p, err := exec.LookPath(bin); err == nil {
			return p
		}
	}

	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".gemini", "antigravity", "bin", "agentapi.bat"),
		filepath.Join(home, ".local", "bin", "agy"),
		filepath.Join(home, ".local", "bin", "antigravity"),
	}

	switch runtime.GOOS {
	case "windows":
		candidates = append(candidates,
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "antigravity", "resources", "bin", "language_server.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "antigravity", "Antigravity.exe"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "antigravity", "bin", "agy.cmd"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "antigravity", "bin", "agy.exe"),
			filepath.Join(home, ".gemini", "bin", "agy.cmd"),
			filepath.Join(home, ".gemini", "bin", "agy.exe"),
			filepath.Join(home, ".local", "bin", "agy.cmd"),
			filepath.Join(home, ".local", "bin", "agy.exe"),
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
					filepath.Join(sysDrive, "\\Users", u, ".gemini", "antigravity", "bin", "agentapi.bat"),
					filepath.Join(sysDrive, "\\Users", u, "AppData", "Local", "Programs", "antigravity", "resources", "bin", "language_server.exe"),
					filepath.Join(sysDrive, "\\Users", u, "AppData", "Local", "Programs", "antigravity", "Antigravity.exe"),
				)
			}
		}
	case "darwin":
		candidates = append(candidates,
			"/Applications/Antigravity.app/Contents/MacOS/Antigravity",
			"/usr/local/bin/agy",
			"/usr/local/bin/antigravity",
		)
	case "linux":
		candidates = append(candidates,
			"/usr/bin/agy",
			"/usr/local/bin/agy",
			"/usr/bin/antigravity",
		)
	}

	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

// resolveAgentAPI 将探测到的可执行路径转换为可直接执行 agentapi 的命令与参数。
func resolveAgentAPI(binPath string) (cmd string, args []string, err error) {
	base := strings.ToLower(filepath.Base(binPath))
	switch {
	case strings.Contains(base, "language_server"):
		return binPath, []string{"agentapi"}, nil
	case strings.Contains(base, "agentapi"):
		return binPath, nil, nil
	case strings.Contains(base, "agy"):
		return binPath, nil, nil
	case strings.Contains(base, "antigravity.exe") || base == "antigravity":
		// 若指向 Electron GUI 入口，优先自动定位同目录下的 resources/bin/language_server.exe
		lsPath := filepath.Join(filepath.Dir(binPath), "resources", "bin", "language_server.exe")
		if st, err := os.Stat(lsPath); err == nil && !st.IsDir() {
			return lsPath, []string{"agentapi"}, nil
		}
		// 尝试 ~/.gemini/antigravity/bin/agentapi.bat
		home, _ := os.UserHomeDir()
		bat := filepath.Join(home, ".gemini", "antigravity", "bin", "agentapi.bat")
		if st, err := os.Stat(bat); err == nil && !st.IsDir() {
			return bat, nil, nil
		}
		return "", nil, errors.New("检测到 Antigravity GUI 入口，未找到底层 agentapi 或 language_server；请配置 providers.antigravity.path 指向 language_server.exe 或 agentapi.bat")
	default:
		return binPath, nil, nil
	}
}

// Start 启动会话。
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
	case info.Path == fakeAgentBinary || filepath.Base(info.Path) == fakeAgentBinary:
		useFake = true
		p.Proc.Allow(info.Path)
	}

	p.mu.Lock()
	p.state = StateStarting
	p.mu.Unlock()

	sid := spec.SessionID
	if sid == "" {
		sid = "ses-antigravity"
	}

	var sess providers.AgentSession
	if useFake {
		sess = &fakeSessionWrapper{p: p, id: sid, acp: p.ACP.Open(sid)}
	} else {
		cmd, args, err := resolveAgentAPI(info.Path)
		if err != nil {
			p.mu.Lock()
			p.state = StateError
			p.mu.Unlock()
			return nil, err
		}
		p.Proc.Allow(cmd)
		realSess := NewRealSession(sid, cmd, args, spec.WorkspacePath)
		sess = realSess
	}

	if err := sess.Start(ctx); err != nil {
		p.mu.Lock()
		p.state = StateError
		p.mu.Unlock()
		return nil, err
	}

	p.mu.Lock()
	p.sessions[sid] = sess
	p.state = StateReady
	p.mu.Unlock()

	return sess, nil
}

func (p *Provider) Stop(ctx context.Context, sessionID string) error {
	p.mu.Lock()
	s, ok := p.sessions[sessionID]
	if ok {
		delete(p.sessions, sessionID)
	}
	p.state = StateStopping
	p.mu.Unlock()
	if !ok {
		return errors.New("session 不存在")
	}

	err := s.Stop(ctx)
	p.mu.Lock()
	p.state = StateInstalled
	p.mu.Unlock()
	return err
}

func (p *Provider) Status(_ context.Context, sessionID string) (*providers.SessionStatus, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.sessions[sessionID]; !ok {
		return &providers.SessionStatus{SessionID: sessionID, State: p.state}, nil
	}
	return &providers.SessionStatus{SessionID: sessionID, State: StateReady}, nil
}

func (p *Provider) State() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

type fakeSessionWrapper struct {
	p   *Provider
	id  string
	acp acp.Session
}

func (w *fakeSessionWrapper) Start(ctx context.Context) error { return w.acp.Start(ctx) }
func (w *fakeSessionWrapper) Send(ctx context.Context, input []byte) (string, error) {
	w.p.mu.Lock()
	w.p.state = StateBusy
	w.p.mu.Unlock()
	reply, err := w.acp.Send(ctx, input)
	w.p.mu.Lock()
	w.p.state = StateReady
	w.p.mu.Unlock()
	return reply, err
}
func (w *fakeSessionWrapper) Stop(ctx context.Context) error { return w.acp.Stop(ctx) }

// Installer V1 Detect-only。
type Installer struct {
	Path string
}

func (i *Installer) Detect(ctx context.Context) (*providers.InstallInfo, error) {
	return NewProvider(nil, i.Path).Detect(ctx)
}

func (i *Installer) Install(context.Context, providers.InstallSpec) error {
	return errors.New("V1 不支持 Registry 下载；请本机安装 Antigravity 或配置 providers.antigravity.path")
}

func (i *Installer) Update(context.Context, providers.UpdateSpec) error {
	return i.Install(context.Background(), providers.InstallSpec{})
}

func (i *Installer) Uninstall(context.Context) error {
	return errors.New("V1 不支持自动卸载")
}
