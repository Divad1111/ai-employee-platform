// Package job 实现 Job 服务、状态机与幂等创建。
// 状态机出边见 docs/DECISIONS.md Q-04。
// 设计依据：设计文档 §15、§84、§92、§112、§113。
package job

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/idgen"
)

// 状态常量。
const (
	StatusCreated         = "CREATED"
	StatusQueued          = "QUEUED"
	StatusAssigned        = "ASSIGNED"
	StatusStarting        = "STARTING"
	StatusRunning         = "RUNNING"
	StatusSuccess         = "SUCCESS"
	StatusFailed          = "FAILED"
	StatusCancelled       = "CANCELLED"
	StatusTimeout         = "TIMEOUT"
	StatusBlocked         = "BLOCKED"
	StatusWaitingApproval = "WAITING_APPROVAL"
	StatusUnknown         = "UNKNOWN"
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
		StatusRunning: true, StatusSuccess: true, StatusFailed: true, StatusCancelled: true, StatusTimeout: true,
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
		StatusRunning: true, StatusSuccess: true, StatusFailed: true, StatusCancelled: true,
	},
}

// 任务来源常量（开放字符串，便于未来持续扩展其它接入端）。
const (
	SourceWeb      = "web"      // 控制台手动创建
	SourceFeishu   = "feishu"   // 飞书机器人消息
	SourceCron     = "cron"     // 定时触发
	SourceCalendar = "calendar" // 日历任务链
	SourceWebhook  = "webhook"  // 入站 Webhook
	SourceAPI      = "api"      // 开放 API
	SourceSystem   = "system"   // 系统内置/初始化
)

// NormalizeSource 规范化任务来源。未指定或空白默认归一化为 "web"。
func NormalizeSource(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return SourceWeb
	}
	return s
}

// 错误。
var (
	ErrNotFound            = errors.New("job 不存在")
	ErrInvalidTransition   = errors.New("非法状态转换")
	ErrInvalidInput        = errors.New("参数无效")
	ErrIdempotencyConflict = errors.New("idempotency_key 冲突且内容不同")
)

// Job 领域对象。
type Job struct {
	ID               string         `json:"id"`
	EmployeeID       string         `json:"employee_id"`
	WorkspaceID      string         `json:"workspace_id"`
	SessionID        string         `json:"session_id"`
	WorkstationID    string         `json:"workstation_id"`
	Prompt           string         `json:"prompt"`
	CreatedBy        string         `json:"created_by"`
	Status           string         `json:"status"`
	Result           string         `json:"result"`
	InputTokens           int64          `json:"input_tokens"`
	OutputTokens          int64          `json:"output_tokens"`
	CachedInputTokens     int64          `json:"cached_input_tokens"`
	CacheWriteInputTokens int64          `json:"cache_write_input_tokens"`
	CacheReadInputTokens  int64          `json:"cache_read_input_tokens"`
	ReasoningOutputTokens int64          `json:"reasoning_output_tokens"`
	TotalTokens           int64          `json:"total_tokens"`
	Agent                 string         `json:"agent"`
	TokenSource           string         `json:"token_source"`
	TokenUsageStatus      string         `json:"token_usage_status"`
	Source                string         `json:"source"`
	IdempotencyKey   string         `json:"idempotency_key"`
	TimeoutSec       int            `json:"timeout_sec"`
	WorkflowID       string         `json:"workflow_id,omitempty"`
	WorkflowSnapshot map[string]any `json:"workflow_snapshot,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	StartedAt        time.Time      `json:"started_at,omitempty"`
	CompletedAt      time.Time      `json:"completed_at,omitempty"`
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
	Source         string `json:"source"`
	WorkflowID     string `json:"workflow_id"`
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
	mu    sync.Mutex
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
	src := NormalizeSource(in.Source)
	j := &Job{
		ID:             idgen.New("JOB"),
		EmployeeID:     in.EmployeeID,
		WorkspaceID:    in.WorkspaceID,
		SessionID:      in.SessionID,
		WorkstationID:  in.WorkstationID,
		Prompt:         in.Prompt,
		CreatedBy:      in.CreatedBy,
		Status:         StatusCreated,
		Source:         src,
		IdempotencyKey: in.IdempotencyKey,
		TimeoutSec:     in.TimeoutSec,
		WorkflowID:     in.WorkflowID,
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
	s.audit.Log(ctx, "USER", actorID, "job.create", "success", ip, map[string]string{
		"id":          j.ID,
		"prompt":      j.Prompt,
		"employee_id": j.EmployeeID,
		"source":      j.Source,
	})
	s.publishStatus(ctx, j)
	return j, false, nil
}

// Transition 状态转换并写 Timeline。
func (s *Service) Transition(ctx context.Context, id, to, actorID, ip string, payload map[string]string) (*Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	if (to == StatusStarting || to == StatusRunning) && j.StartedAt.IsZero() {
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
	actorType := "USER"
	if actorID == "workstation" || strings.HasPrefix(actorID, "WS-") {
		actorType = "WORKSTATION"
	} else if actorID == "scheduler" || actorID == "system" || actorID == "permission" {
		actorType = "SYSTEM"
	}
	s.audit.Log(ctx, actorType, actorID, "job.transition", "success", ip, map[string]string{
		"id":          id,
		"to":          to,
		"prompt":      j.Prompt,
		"employee_id": j.EmployeeID,
		"source":      j.Source,
	})
	if terminal[to] {
		auditResult := "success"
		if to == StatusFailed || to == StatusTimeout || to == StatusUnknown {
			auditResult = "failed"
		}
		execActorType := "USER"
		execActorID := j.CreatedBy
		if execActorID == "" {
			execActorID = actorID
			execActorType = actorType
		}
		s.audit.Log(ctx, execActorType, execActorID, "job.execute", auditResult, ip, map[string]string{
			"id":             id,
			"status":         to,
			"prompt":         j.Prompt,
			"employee_id":    j.EmployeeID,
			"source":         j.Source,
			"workstation_id": j.WorkstationID,
		})
	}
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

// ListEvents 返回任务时间线，按写入顺序。
func (s *Service) ListEvents(ctx context.Context, jobID string) ([]*Event, error) {
	return s.store.ListEvents(ctx, jobID)
}

// BindWorkstation 绑定执行节点。
func (s *Service) BindWorkstation(ctx context.Context, jobID, wsID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.WorkstationID = wsID
	return s.store.Save(ctx, j)
}

// BindSession 绑定运行会话。
func (s *Service) BindSession(ctx context.Context, jobID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.SessionID = sessionID
	return s.store.Save(ctx, j)
}

// SetResult 写入 Job 结果文本（Agent 回复）。
func (s *Service) SetResult(ctx context.Context, jobID, result string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.Result = result
	return s.store.Save(ctx, j)
}

// TokenSummary 任务汇总缓存写入参数。
type TokenSummary struct {
	InputTokens           int64
	OutputTokens          int64
	CachedInputTokens     int64
	CacheWriteInputTokens int64
	CacheReadInputTokens  int64
	ReasoningOutputTokens int64
	TotalTokens           int64
	UsageStatus           string
}

// ApplyTokenSummary 用明细汇总刷新 jobs 缓存字段（可重复调用）。
func (s *Service) ApplyTokenSummary(ctx context.Context, jobID string, sum TokenSummary, agentName, source string) (createdBy string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return "", ErrNotFound
	}
	j.InputTokens = sum.InputTokens
	j.OutputTokens = sum.OutputTokens
	j.CachedInputTokens = sum.CachedInputTokens
	j.CacheWriteInputTokens = sum.CacheWriteInputTokens
	j.CacheReadInputTokens = sum.CacheReadInputTokens
	j.ReasoningOutputTokens = sum.ReasoningOutputTokens
	j.TotalTokens = sum.TotalTokens
	if j.TotalTokens <= 0 {
		j.TotalTokens = j.InputTokens + j.OutputTokens
	}
	j.TokenUsageStatus = sum.UsageStatus
	if agentName != "" {
		j.Agent = agentName
	}
	if source != "" && source != "estimate" {
		j.TokenSource = source
	}
	if j.TokenSource == "estimate" {
		j.TokenSource = "unavailable"
		if j.TokenUsageStatus == "" {
			j.TokenUsageStatus = "UNAVAILABLE"
		}
	}
	if err := s.store.Save(ctx, j); err != nil {
		return j.CreatedBy, err
	}
	return j.CreatedBy, nil
}

// RecordTokens 兼容旧事件：单次写入汇总缓存。真实明细请走 tokenusage.Service。
func (s *Service) RecordTokens(ctx context.Context, jobID string, input, output int64, agentName, source string) (createdBy string, first bool, err error) {
	if source == "estimate" {
		source = "unavailable"
		input, output = 0, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return "", false, ErrNotFound
	}
	if j.TokenUsageStatus != "" || (j.TokenSource != "" && j.TokenSource != "estimate") {
		return j.CreatedBy, false, nil
	}
	j.InputTokens = input
	j.OutputTokens = output
	j.TotalTokens = input + output
	j.Agent = agentName
	j.TokenSource = source
	if j.TokenSource == "" {
		j.TokenSource = "unavailable"
	}
	if input == 0 && output == 0 {
		j.TokenUsageStatus = "UNAVAILABLE"
	} else {
		j.TokenUsageStatus = "FINAL"
	}
	if err := s.store.Save(ctx, j); err != nil {
		return j.CreatedBy, false, err
	}
	return j.CreatedBy, true, nil
}

// SetWorkflowSnapshot 设置工作流关联及快照。
func (s *Service) SetWorkflowSnapshot(ctx context.Context, jobID, workflowID string, snapshot map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, err := s.store.Get(ctx, jobID)
	if err != nil || j == nil {
		return ErrNotFound
	}
	j.WorkflowID = workflowID
	j.WorkflowSnapshot = snapshot
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
