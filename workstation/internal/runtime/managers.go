// Package runtime 聚合本地 Employee / Workspace / Session / Job 管理。
// 设计依据：设计文档 §39、§49、§54、§56、§120。
package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/platform"
)

// 状态常量。
const (
	JobCreated   = "CREATED"
	JobRunning   = "RUNNING"
	JobSuccess   = "SUCCESS"
	JobFailed    = "FAILED"
	JobUnknown   = "UNKNOWN"
	SessStarting = "STARTING"
	SessReady    = "READY"
	SessBusy     = "BUSY"
	SessStopped  = "STOPPED"
	SessUnknown  = "UNKNOWN"
)

var (
	ErrActiveSession = errors.New("已有 Active Session")
	ErrNotFound      = errors.New("不存在")
	ErrLocked        = errors.New("workspace 锁定中")
)

// Employee 本地视图。
type Employee struct {
	ID       string
	Name     string
	Provider string
	Dir      string
}

// Workspace 本地视图。
type Workspace struct {
	ID         string
	EmployeeID string
	Path       string
	LockedBy   string // session_id
}

// Session 本地会话。
type Session struct {
	ID          string
	EmployeeID  string
	WorkspaceID string
	Provider    string
	Status      string
	PID         int
}

// Job 本地任务。
type Job struct {
	ID         string
	EmployeeID string
	SessionID  string
	Prompt     string
	Status     string
}

// EventSink 上报事件（Outbox / 测试）。
type EventSink func(typ string, payload map[string]string)

// Managers 本地运行时门面。
type Managers struct {
	Paths    platform.Paths
	Registry *providers.Registry
	MaxSess  int
	Sink     EventSink

	mu         sync.Mutex
	employees  map[string]*Employee
	workspaces map[string]*Workspace
	sessions   map[string]*Session
	jobs       map[string]*Job
	agentSess  map[string]providers.AgentSession
}

// NewManagers 创建。
func NewManagers(paths platform.Paths, reg *providers.Registry) *Managers {
	return &Managers{
		Paths:      paths,
		Registry:   reg,
		MaxSess:    1,
		employees:  map[string]*Employee{},
		workspaces: map[string]*Workspace{},
		sessions:   map[string]*Session{},
		jobs:       map[string]*Job{},
		agentSess:  map[string]providers.AgentSession{},
	}
}

// EnsureEmployee 以 Employee ID 为键创建本地目录（§49）。
func (m *Managers) EnsureEmployee(id, name, provider string) (*Employee, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.employees[id]; ok {
		return e, nil
	}
	dir := filepath.Join(m.Paths.WorkspaceRoot(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	e := &Employee{ID: id, Name: name, Provider: provider, Dir: dir}
	if e.Provider == "" {
		e.Provider = "cursor"
	}
	m.employees[id] = e
	return e, nil
}

// EnsureWorkspace 绑定路径；Q-01 独占。
func (m *Managers) EnsureWorkspace(id, employeeID, path string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if path == "" {
		path = filepath.Join(m.Paths.WorkspaceRoot(), employeeID, "workspace")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return nil, err
	}
	w := &Workspace{ID: id, EmployeeID: employeeID, Path: path}
	m.workspaces[id] = w
	return w, nil
}

// StartSession 按需启动；无 Job 时可不跑（调用方决定）。
func (m *Managers) StartSession(ctx context.Context, sessID, employeeID, workspaceID, provider string) (*Session, error) {
	m.mu.Lock()
	for _, s := range m.sessions {
		if s.EmployeeID == employeeID && s.Status != SessStopped && s.Status != SessUnknown {
			m.mu.Unlock()
			return nil, ErrActiveSession
		}
	}
	w := m.workspaces[workspaceID]
	if w != nil && w.LockedBy != "" && w.LockedBy != sessID {
		m.mu.Unlock()
		return nil, ErrLocked
	}
	emp := m.employees[employeeID]
	if provider == "" && emp != nil {
		provider = emp.Provider
	}
	if provider == "" {
		provider = "cursor"
	}
	wsPath := ""
	if w != nil {
		wsPath = w.Path
		w.LockedBy = sessID
	}
	s := &Session{
		ID: sessID, EmployeeID: employeeID, WorkspaceID: workspaceID,
		Provider: provider, Status: SessStarting,
	}
	m.sessions[sessID] = s
	m.mu.Unlock()

	prov, err := m.Registry.Get(provider)
	if err != nil {
		m.markSession(sessID, SessUnknown)
		return nil, err
	}
	agent, err := prov.Start(ctx, providers.StartSpec{
		EmployeeID: employeeID, WorkspacePath: wsPath, SessionID: sessID,
	})
	if err != nil {
		m.markSession(sessID, SessUnknown)
		return nil, err
	}
	m.mu.Lock()
	m.agentSess[sessID] = agent
	s.Status = SessReady
	m.mu.Unlock()
	m.emit("session.status", map[string]string{"session_id": sessID, "status": SessReady})
	return s, nil
}

// FindActiveSession 查找员工当前活跃 Session。
func (m *Managers) FindActiveSession(employeeID string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.EmployeeID == employeeID && s.Status != SessStopped && s.Status != SessUnknown {
			return s
		}
	}
	return nil
}

func (m *Managers) markSession(id, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Status = status
	}
}

// RunJob 在已有或按需 Session 上跑 Job；完成后不强制关 Session（可复用）。
// 返回的 Job.Prompt 对应输入；回复文本通过第二返回值给出。
func (m *Managers) RunJob(ctx context.Context, jobID, employeeID, sessionID, prompt string) (*Job, string, error) {
	m.mu.Lock()
	j := &Job{ID: jobID, EmployeeID: employeeID, SessionID: sessionID, Prompt: prompt, Status: JobRunning}
	m.jobs[jobID] = j
	agent := m.agentSess[sessionID]
	if s, ok := m.sessions[sessionID]; ok {
		s.Status = SessBusy
	}
	m.mu.Unlock()
	m.emit("job.status", map[string]string{"job_id": jobID, "status": JobRunning})

	if agent == nil {
		m.setJob(jobID, JobFailed)
		return j, "", fmt.Errorf("session 未就绪")
	}
	reply, err := agent.Send(ctx, []byte(prompt))
	if err != nil {
		m.setJob(jobID, JobFailed)
		m.markSession(sessionID, SessReady)
		return j, reply, err
	}
	m.setJob(jobID, JobSuccess)
	m.markSession(sessionID, SessReady)
	m.emit("job.status", map[string]string{"job_id": jobID, "status": JobSuccess})
	return j, reply, nil
}

func (m *Managers) setJob(id, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		j.Status = status
	}
}

// MarkUnknown 崩溃恢复：不标 SUCCESS。
func (m *Managers) MarkUnknown(sessionID, jobID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if sessionID != "" {
		if s, ok := m.sessions[sessionID]; ok && s.Status != SessStopped {
			s.Status = SessUnknown
		}
	}
	if jobID != "" {
		if j, ok := m.jobs[jobID]; ok && j.Status == JobRunning {
			j.Status = JobUnknown
		}
	}
}

// StopSession 停止并释放 Workspace 锁。
func (m *Managers) StopSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionID]
	agent := m.agentSess[sessionID]
	delete(m.agentSess, sessionID)
	if ok {
		if w, wok := m.workspaces[s.WorkspaceID]; wok && w.LockedBy == sessionID {
			w.LockedBy = ""
		}
		s.Status = SessStopped
		provName := s.Provider
		m.mu.Unlock()
		if agent != nil {
			_ = agent.Stop(ctx)
		}
		if p, err := m.Registry.Get(provName); err == nil {
			_ = p.Stop(ctx, sessionID)
		}
		m.emit("session.status", map[string]string{"session_id": sessionID, "status": SessStopped})
		return nil
	}
	m.mu.Unlock()
	return ErrNotFound
}

// Snapshot 状态快照。
func (m *Managers) Snapshot() (emps, wss, sess, jobs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.employees), len(m.workspaces), len(m.sessions), len(m.jobs)
}

func (m *Managers) GetSession(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func (m *Managers) GetJob(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	cp := *j
	return &cp, true
}

func (m *Managers) ListSessions() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		cp := *s
		out = append(out, &cp)
	}
	return out
}

func (m *Managers) ListJobs() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		cp := *j
		out = append(out, &cp)
	}
	return out
}

func (m *Managers) emit(typ string, payload map[string]string) {
	if m.Sink != nil {
		m.Sink(typ, payload)
	}
}

// RecoverRunning 将仍标记 RUNNING 的 Job / 非 STOPPED Session 标 UNKNOWN（重启恢复）。
func (m *Managers) RecoverRunning() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.Status != SessStopped && s.Status != SessUnknown {
			s.Status = SessUnknown
		}
	}
	for _, j := range m.jobs {
		if j.Status == JobRunning {
			j.Status = JobUnknown
		}
	}
}
