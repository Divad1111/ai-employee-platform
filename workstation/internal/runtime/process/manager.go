// Package process 提供受控进程启停，禁止任意 Shell 执行。
// 设计依据：设计文档 §116、§60、§88。
package process

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// 错误。
var (
	ErrNotAllowed = errors.New("进程规格不在白名单")
	ErrNotFound   = errors.New("进程不存在")
	ErrAlreadyRun = errors.New("进程已在运行")
)

// Spec 白名单业务进程规格（无任意命令字符串）。
type Spec struct {
	ID      string   // 业务 ID（如 session_id）
	Name    string   // 逻辑名（cursor/codex）
	Binary  string   // 绝对路径可执行文件
	Args    []string // 固定参数列表
	WorkDir string
	Env     []string // 额外环境变量 KEY=VAL
}

// Info 运行中进程信息。
type Info struct {
	ID        string
	Name      string
	PID       int
	StartedAt time.Time
	Running   bool
}

// Manager 受控进程管理。
type Manager struct {
	mu      sync.Mutex
	allowed map[string]bool // 允许的 binary 基名或绝对路径
	procs   map[string]*tracked
	runner  Runner // 可注入，便于测试
}

type tracked struct {
	spec Spec
	info Info
	kill func() error
}

// Runner 抽象启动（测试可替换）。
type Runner interface {
	Start(ctx context.Context, spec Spec) (pid int, wait func() error, kill func() error, err error)
}

// NewManager 创建；allowedBinaries 为允许的可执行文件基名或绝对路径。
func NewManager(allowedBinaries ...string) *Manager {
	m := &Manager{
		allowed: map[string]bool{},
		procs:   map[string]*tracked{},
		runner:  OSRunner{},
	}
	for _, b := range allowedBinaries {
		m.allowed[filepath.Base(b)] = true
		m.allowed[b] = true
	}
	return m
}

// SetRunner 注入 Runner。
func (m *Manager) SetRunner(r Runner) { m.runner = r }

// Allow 动态加入白名单。
func (m *Manager) Allow(binary string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.allowed[filepath.Base(binary)] = true
	m.allowed[binary] = true
}

func (m *Manager) check(spec Spec) error {
	if spec.ID == "" || spec.Binary == "" {
		return ErrNotAllowed
	}
	base := filepath.Base(spec.Binary)
	if !m.allowed[spec.Binary] && !m.allowed[base] {
		return fmt.Errorf("%w: %s", ErrNotAllowed, spec.Binary)
	}
	return nil
}

// Start 启动白名单进程。
func (m *Manager) Start(ctx context.Context, spec Spec) (*Info, error) {
	if err := m.check(spec); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.procs[spec.ID]; ok && t.info.Running {
		return nil, ErrAlreadyRun
	}
	pid, wait, kill, err := m.runner.Start(ctx, spec)
	if err != nil {
		return nil, err
	}
	info := Info{ID: spec.ID, Name: spec.Name, PID: pid, StartedAt: time.Now().UTC(), Running: true}
	t := &tracked{spec: spec, info: info, kill: kill}
	m.procs[spec.ID] = t
	go func() {
		_ = wait()
		m.mu.Lock()
		if cur, ok := m.procs[spec.ID]; ok && cur.info.PID == pid {
			cur.info.Running = false
		}
		m.mu.Unlock()
	}()
	cp := info
	return &cp, nil
}

// Stop 停止进程。
func (m *Manager) Stop(_ context.Context, id string) error {
	m.mu.Lock()
	t, ok := m.procs[id]
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if t.kill != nil {
		_ = t.kill()
	}
	m.mu.Lock()
	if cur, ok := m.procs[id]; ok {
		cur.info.Running = false
	}
	m.mu.Unlock()
	return nil
}

// Inspect 查询。
func (m *Manager) Inspect(id string) (*Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.procs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := t.info
	// 若 PID 消失则标记未运行
	if cp.Running && cp.PID > 0 {
		if proc, err := os.FindProcess(cp.PID); err == nil {
			// Windows FindProcess 总成功；用 Signal(0) 在 Unix 探测
			if err := signalAlive(proc); err != nil {
				cp.Running = false
				t.info.Running = false
			}
		}
	}
	return &cp, nil
}

// OSRunner 真实进程。
type OSRunner struct{}

func (OSRunner) Start(ctx context.Context, spec Spec) (int, func() error, func() error, error) {
	cmd := exec.CommandContext(ctx, spec.Binary, spec.Args...)
	if spec.WorkDir != "" {
		cmd.Dir = spec.WorkDir
	}
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, nil, err
	}
	pid := cmd.Process.Pid
	wait := func() error { return cmd.Wait() }
	kill := func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	return pid, wait, kill, nil
}

// FakeRunner 测试用：不启动真实进程。
type FakeRunner struct {
	NextPID int
	Started []Spec
	Alive   map[string]bool
}

func (f *FakeRunner) Start(_ context.Context, spec Spec) (int, func() error, func() error, error) {
	if f.NextPID == 0 {
		f.NextPID = 1000
	}
	if f.Alive == nil {
		f.Alive = map[string]bool{}
	}
	f.NextPID++
	pid := f.NextPID
	f.Started = append(f.Started, spec)
	f.Alive[spec.ID] = true
	done := make(chan struct{})
	var once sync.Once
	wait := func() error { <-done; return nil }
	kill := func() error {
		f.Alive[spec.ID] = false
		once.Do(func() { close(done) })
		return nil
	}
	return pid, wait, kill, nil
}
