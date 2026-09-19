// Package job 实现 Job 服务、状态机与幂等创建。
// 状态机出边见 docs/DECISIONS.md Q-04。
// 设计依据：设计文档 §15、§84、§92、§112、§113。
package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/idgen"
)

// 状态常量。
const (
	StatusCreated          = "CREATED"
	StatusQueued           = "QUEUED"
	StatusAssigned         = "ASSIGNED"
	StatusStarting         = "STARTING"
	StatusRunning          = "RUNNING"
	StatusSuccess          = "SUCCESS"
	StatusFailed           = "FAILED"
	StatusCancelled        = "CANCELLED"
	StatusTimeout          = "TIMEOUT"
	StatusBlocked          = "BLOCKED"
	StatusWaitingApproval  = "WAITING_APPROVAL"
	StatusUnknown          = "UNKNOWN"
)

var terminal = map[string]bool{
	StatusSuccess: true, StatusFailed: true, StatusCancelled: true, StatusTimeout: true,
}

// 合法转换（Q-04）。
var transitions = map[string]map[string]bool{
	StatusCreated: {
		StatusQueued: true, StatusCancelled: true,
	},
	StatusQueued: {
		StatusAssigned: true, StatusCancelled: true,
	},
	StatusAssigned: {
		StatusStarting: true, StatusCancelled: true,
	},
	StatusStarting: {
		StatusRunning: true, StatusFailed: true, StatusCancelled: true, StatusTimeout: true,
	},
	StatusRunning: {
		StatusSuccess: true, StatusFailed: true, StatusCancelled: true, StatusTimeout: true,
		StatusBlocked: true, StatusWaitingApproval: true, StatusUnknown: true,
	},
	StatusBlocked: {
		StatusRunning: true, StatusCancelled: true, StatusFailed: true,
	},
	StatusWaitingApproval: {
		StatusRunning: true, StatusCancelled: true, StatusFailed: true,
	},
	StatusUnknown: {
		StatusRunning: true, StatusFailed: true, StatusCancelled: true,
	},
}

// 错误。
var (
	ErrNotFound          = errors.New("job 不存在")
	ErrInvalidTransition = errors.New("非法状态转换")
	ErrInvalidInput      = errors.New("参数无效")
	ErrIdempotencyConflict = errors.New("idempotency_key 冲突且内容不同")
)

// Job 领域对象。
type Job struct {
	ID             string    `json:"id"`
	EmployeeID     string    `json:"employee_id"`
	WorkspaceID    string    `json:"workspace_id"`
	SessionID      string    `json:"session_id"`
	WorkstationID  string    `json:"workstation_id"`
	Prompt         string    `json:"prompt"`
	CreatedBy      string    `json:"created_by"`
	Status         string    `json:"status"`
	Result         string    `json:"result"`
	IdempotencyKey string    `json:"idempotency_key"`
	TimeoutSec     int       `json:"timeout_sec"`
	CreatedAt      time.Time `json:"created_at"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
}

// Event Timeline 条目。
type Event struct {
	ID        int64             `json:"id"`
	JobID     string            `json:"job_id"`
	EventType string            `json:"event_type"`
	Payload   map[string]string `json:"payload"`
	CreatedAt time.Time         `json:"created_at"`
}

// CreateInput 创建。
type CreateInput struct {
	EmployeeID     string `json:"employee_id"`
	WorkspaceID    string `json:"workspace_id"`
	SessionID      string `json:"session_id"`
	WorkstationID  string `json:"workstation_id"`
	Prompt         string `json:"prompt"`
	IdempotencyKey string `json:"idempotency_key"`
	TimeoutSec     int    `json:"timeout_sec"`
	CreatedBy      string `json:"created_by"`
}

// Auditor / Bus。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

type Bus interface {
	Publish(ctx context.Context, typ string, payload map[string]string) eventbus.Event
}

// Store 持久化。
type Store interface {
	Save(ctx context.Context, j *Job) error
	Get(ctx context.Context, id string) (*Job, error)
	GetByIdempotency(ctx context.Context, key string) (*Job, error)
	List(ctx context.Context) ([]*Job, error)
	AppendEvent(ctx context.Context, e *Event) error
	ListEvents(ctx context.Context, jobID string) ([]*Event, error)
}

// Service Job 服务。
type Service struct {
	store Store
	audit Auditor
	bus   Bus
}

func NewService(store Store, audit Auditor, bus Bus) *Service {
	return &Service{store: store, audit: audit, bus: bus}
}

// Create 幂等创建：相同 idempotency_key 返回已有 Job。
func (s *Service) Create(ctx context.Context, in CreateInput, actorID, ip string) (*Job, bool, error) {
	if in.EmployeeID == "" || in.IdempotencyKey == "" {
		return nil, false, ErrInvalidInput
	}
	if existing, _ := s.store.GetByIdempotency(ctx, in.IdempotencyKey); existing != nil {
		if existing.EmployeeID != in.EmployeeID || existing.Prompt != in.Prompt {
			return nil, false, ErrIdempotencyConflict
		}
		return existing, true, nil
	}
	now := time.Now().UTC()
	j := &Job{
		ID:             idgen.New("JOB"),
		EmployeeID:     in.EmployeeID,
		WorkspaceID:    in.WorkspaceID,
		SessionID:      in.SessionID,
		WorkstationID:  in.WorkstationID,
		Prompt:         in.Prompt,
		CreatedBy:      in.CreatedBy,
		Status:         StatusCreated,
		IdempotencyKey: in.IdempotencyKey,
		TimeoutSec:     in.TimeoutSec,
		CreatedAt:      now,
	}
	if j.CreatedBy == "" {
		j.CreatedBy = actorID
	}
	if err := s.store.Save(ctx, j); err != nil {
		return nil, false, err
	}
	_ = s.store.AppendEvent(ctx, &Event{
		JobID: j.ID, EventType: "CREATED", Payload: map[string]string{"status": StatusCreated}, CreatedAt: now,
	})
	s.audit.Log(ctx, "USER", actorID, "job.create", "success", ip, map[string]string{"id": j.ID})
	s.publishStatus(ctx, j)
	return j, false, nil
}

// Transition 状态转换并写 Timeline。
func (s *Service) Transition(ctx context.Context, id, to, actorID, ip string, payload map[string]string) (*Job, error) {
	j, err := s.store.Get(ctx, id)
	if err != nil || j == nil {
		return nil, ErrNotFound
	}
	if terminal[j.Status] {
		return nil, fmt.Errorf("%w: 已终态 %s", ErrInvalidTransition, j.Status)
	}
	allowed := transitions[j.Status]
	if allowed == nil || !allowed[to] {
		return nil, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, j.Status, to)
	}
	from := j.Status
	j.Status = to
	now := time.Now().UTC()
	if to == StatusRunning && j.StartedAt.IsZero() {
		j.StartedAt = now
	}
	if terminal[to] {
		j.CompletedAt = now
	}
	if err := s.store.Save(ctx, j); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]string{}
	}
	payload["from"] = from
	payload["to"] = to
	_ = s.store.AppendEvent(ctx, &Event{
		JobID: id, EventType: "STATUS", Payload: payload, CreatedAt: now,
	})
	s.audit.Log(ctx, "USER", actorID, "job.transition", "success", ip, map[string]string{"id": id, "to": to})
	s.publishStatus(ctx, j)
	return j, nil
}

// Cancel 取消。
func (s *Service) Cancel(ctx context.Context, id, actorID, ip string) (*Job, error) {
	return s.Transition(ctx, id, StatusCancelled, actorID, ip, map[string]string{"reason": "cancel"})
}

// MarkUnknown Workstation Offline 时由调度调用。
func (s *Service) MarkUnknown(ctx context.Context, id, actorID, ip string) (*Job, error) {
	return s.Transition(ctx, id, StatusUnknown, actorID, ip, map[string]string{"reason": "offline"})
}

func (s *Service) Get(ctx context.Context, id string) (*Job, error) {
	j, err := s.store.Get(ctx, id)
	if err != nil || j == nil {
		return nil, ErrNotFound
	}
	return j, nil
}

func (s *Service) List(ctx context.Context) ([]*Job, error) {
	return s.store.List(ctx)
}

// BindWorkstation 绑定执行节点。
func (s *Service) BindWorkstation(ctx context.Context, jobID, wsID string) error {
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.WorkstationID = wsID
	return s.store.Save(ctx, j)
}

// BindSession 绑定运行会话。
func (s *Service) BindSession(ctx context.Context, jobID, sessionID string) error {
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.SessionID = sessionID
	return s.store.Save(ctx, j)
}

// SetResult 写入 Job 结果文本（Agent 回复）。
func (s *Service) SetResult(ctx context.Context, jobID, result string) error {
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.Result = result
	return s.store.Save(ctx, j)
}

func (s *Service) Timeline(ctx context.Context, id string) ([]*Event, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	return s.store.ListEvents(ctx, id)
}

// AppendEvent 写入 Timeline（Approval 等旁路事件）。
func (s *Service) AppendEvent(ctx context.Context, jobID, eventType string, payload map[string]string) error {
	if _, err := s.Get(ctx, jobID); err != nil {
		return err
	}
	return s.store.AppendEvent(ctx, &Event{
		JobID: jobID, EventType: eventType, Payload: payload, CreatedAt: time.Now().UTC(),
	})
}

// ActiveCount 非终态 Job 数。
func (s *Service) ActiveCount(ctx context.Context) (int, error) {
	list, err := s.store.List(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, j := range list {
		if !terminal[j.Status] {
			n++
		}
	}
	return n, nil
}

func (s *Service) publishStatus(ctx context.Context, j *Job) {
	if s.bus == nil {
		return
	}
	s.bus.Publish(ctx, eventbus.TypeJobStatus, map[string]string{
		"job_id": j.ID, "status": j.Status, "employee_id": j.EmployeeID,
	})
}

// MemoryStore 内存。
type MemoryStore struct {
	mu       sync.RWMutex
	byID     map[string]*Job
	byIdem   map[string]string
	events   map[string][]*Event
	nextEvID int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		byID: map[string]*Job{}, byIdem: map[string]string{}, events: map[string][]*Event{},
	}
}

func (m *MemoryStore) Save(_ context.Context, j *Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *j
	m.byID[j.ID] = &cp
	m.byIdem[j.IdempotencyKey] = j.ID
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	j, ok := m.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *j
	return &cp, nil
}

func (m *MemoryStore) GetByIdempotency(_ context.Context, key string) (*Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.byIdem[key]
	if !ok {
		return nil, nil
	}
	j := m.byID[id]
	cp := *j
	return &cp, nil
}

func (m *MemoryStore) List(_ context.Context) ([]*Job, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Job, 0, len(m.byID))
	for _, j := range m.byID {
		cp := *j
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryStore) AppendEvent(_ context.Context, e *Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextEvID++
	cp := *e
	cp.ID = m.nextEvID
	if cp.Payload != nil {
		p := map[string]string{}
		for k, v := range cp.Payload {
			p[k] = v
		}
		cp.Payload = p
	}
	m.events[e.JobID] = append(m.events[e.JobID], &cp)
	return nil
}

func (m *MemoryStore) ListEvents(_ context.Context, jobID string) ([]*Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.events[jobID]
	out := make([]*Event, len(src))
	copy(out, src)
	return out, nil
}
