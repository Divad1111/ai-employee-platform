// Package taskconversation 管理由用户任务创建、可多轮续聊的飞书会话。
package taskconversation

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("任务会话不存在")
	ErrExists   = errors.New("任务卡片已存在")
)

// Conversation 是一张飞书卡片所代表的用户任务会话。
type Conversation struct {
	ID              string    `json:"id"`
	EmployeeID      string    `json:"employee_id"`
	EmployeeName    string    `json:"employee_name,omitempty"`
	Provider        string    `json:"provider,omitempty"`
	Model           string    `json:"model,omitempty"`
	DefaultProvider string    `json:"default_provider,omitempty"`
	DefaultModel    string    `json:"default_model,omitempty"`
	OwnerUserID     string    `json:"owner_user_id"`
	OwnerOpenID     string    `json:"owner_open_id"`
	ChatID          string    `json:"chat_id"`
	CurrentJobID    string    `json:"current_job_id"`
	CardMessageID   string    `json:"card_message_id,omitempty"`
	Status          string    `json:"status"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Message 是服务端保存的一条完整对话消息。
type Message struct {
	ConversationID    string    `json:"conversation_id"`
	JobID             string    `json:"job_id"`
	TurnNo            int       `json:"turn_no"`
	SessionGeneration int64     `json:"session_generation"`
	Role              string    `json:"role"`
	Content           string    `json:"content"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

// Activity 是一条面向用户的执行过程摘要，不包含 Agent 内部思考内容。
type Activity struct {
	JobID     string    `json:"job_id"`
	Sequence  uint64    `json:"sequence"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// Store 保存会话映射、完整消息历史及可幂等合并的回复增量。
type Store interface {
	CreateTask(context.Context, *Conversation, string, int64) error
	Get(context.Context, string) (*Conversation, error)
	FindByJob(context.Context, string) (*Conversation, error)
	ListIDs(context.Context, int) ([]string, error)
	ListEmployeeIDs(context.Context, string, int) ([]string, error)
	ListOwnerChatIDs(context.Context, string, string, int) ([]string, error)
	SessionJobIDs(context.Context, string) ([]string, error)
	SessionMessages(context.Context, string, int64) ([]Message, error)
	SetRuntime(context.Context, string, string, string, int64) error
	SetCardMessageID(context.Context, string, string) error
	SetTurnStatus(context.Context, string, string) error
	AppendDelta(context.Context, string, string, uint64, string) error
	AppendActivity(context.Context, string, string, uint64, string) error
	SetAssistantFinal(context.Context, string, string, string) error
	Messages(context.Context, string) ([]Message, error)
	Activities(context.Context, string, string, int) ([]Activity, error)
}

type memoryTurn struct {
	jobID             string
	no                int
	state             string
	sessionGeneration int64
}

// MemoryStore 是本地开发与无数据库模式的会话存储。
type MemoryStore struct {
	mu             sync.RWMutex
	convs          map[string]*Conversation
	byJob          map[string]string
	turns          map[string][]memoryTurn
	messages       map[string][]Message
	deltas         map[string]map[uint64]string
	deltaEvents    map[string]struct{}
	activities     map[string][]Activity
	activityEvents map[string]struct{}
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		convs: make(map[string]*Conversation), byJob: make(map[string]string),
		turns: make(map[string][]memoryTurn), messages: make(map[string][]Message),
		deltas: make(map[string]map[uint64]string), deltaEvents: make(map[string]struct{}),
		activities: make(map[string][]Activity), activityEvents: make(map[string]struct{}),
	}
}

func (m *MemoryStore) CreateTask(_ context.Context, c *Conversation, prompt string, generation int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing := m.byJob[c.CurrentJobID]; existing != "" {
		if existing == c.ID {
			return nil
		}
		return ErrExists
	}
	cp := *c
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = time.Now().UTC()
	}
	if cp.UpdatedAt.IsZero() {
		cp.UpdatedAt = cp.CreatedAt
	}
	m.convs[c.ID] = &cp
	now := time.Now().UTC()
	m.turns[c.ID] = append(m.turns[c.ID], memoryTurn{jobID: c.CurrentJobID, no: 1, state: "CREATED", sessionGeneration: generation})
	m.byJob[c.CurrentJobID] = c.ID
	m.messages[c.ID] = append(m.messages[c.ID],
		Message{ConversationID: c.ID, JobID: c.CurrentJobID, TurnNo: 1, SessionGeneration: generation, Role: "user", Content: prompt, Status: "DONE", CreatedAt: now},
		Message{ConversationID: c.ID, JobID: c.CurrentJobID, TurnNo: 1, SessionGeneration: generation, Role: "assistant", Status: "RUNNING", CreatedAt: now},
	)
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.convs[id]
	if c == nil {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *MemoryStore) FindByJob(_ context.Context, jobID string) (*Conversation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id := m.byJob[jobID]
	if id == "" || m.convs[id] == nil {
		return nil, ErrNotFound
	}
	cp := *m.convs[id]
	return &cp, nil
}

func (m *MemoryStore) ListIDs(_ context.Context, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*Conversation, 0, len(m.convs))
	for _, c := range m.convs {
		items = append(items, c)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

func (m *MemoryStore) ListEmployeeIDs(_ context.Context, employeeID string, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	items := make([]*Conversation, 0)
	for _, c := range m.convs {
		if c.EmployeeID == employeeID {
			items = append(items, c)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].UpdatedAt.After(items[j].UpdatedAt) })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

func (m *MemoryStore) ListOwnerChatIDs(_ context.Context, chatID, ownerOpenID string, limit int) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var items []*Conversation
	for _, c := range m.convs {
		if c.ChatID == chatID && c.OwnerOpenID == ownerOpenID {
			items = append(items, c)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		ids = append(ids, c.ID)
	}
	return ids, nil
}

func (m *MemoryStore) SessionJobIDs(_ context.Context, employeeID string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type orderedJob struct {
		id string
		at time.Time
	}
	var jobs []orderedJob
	for conversationID, turns := range m.turns {
		c := m.convs[conversationID]
		if c == nil || c.EmployeeID != employeeID {
			continue
		}
		for _, turn := range turns {
			jobs = append(jobs, orderedJob{id: turn.jobID, at: c.CreatedAt})
		}
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].at.Before(jobs[j].at) })
	out := make([]string, 0, len(jobs))
	for _, item := range jobs {
		out = append(out, item.id)
	}
	return out, nil
}

func (m *MemoryStore) SessionMessages(_ context.Context, employeeID string, generation int64) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Message
	for conversationID, messages := range m.messages {
		c := m.convs[conversationID]
		if c == nil || c.EmployeeID != employeeID {
			continue
		}
		for _, message := range messages {
			if message.SessionGeneration == generation {
				out = append(out, message)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryStore) SetRuntime(_ context.Context, jobID, provider, model string, generation int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.byJob[jobID]
	if id == "" || m.convs[id] == nil {
		return ErrNotFound
	}
	m.convs[id].Provider = provider
	m.convs[id].Model = model
	for i := range m.turns[id] {
		if m.turns[id][i].jobID == jobID {
			m.turns[id][i].sessionGeneration = generation
		}
	}
	for i := range m.messages[id] {
		if m.messages[id][i].JobID == jobID {
			m.messages[id][i].SessionGeneration = generation
		}
	}
	m.convs[id].UpdatedAt = time.Now().UTC()
	return nil
}

func (m *MemoryStore) SetCardMessageID(_ context.Context, id, messageID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.convs[id]
	if c == nil {
		return ErrNotFound
	}
	c.CardMessageID = messageID
	c.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *MemoryStore) SetTurnStatus(_ context.Context, jobID, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.byJob[jobID]
	if id == "" {
		return ErrNotFound
	}
	for i := range m.turns[id] {
		if m.turns[id][i].jobID == jobID {
			m.turns[id][i].state = status
		}
	}
	for i := range m.messages[id] {
		if m.messages[id][i].JobID == jobID && m.messages[id][i].Role == "assistant" {
			m.messages[id][i].Status = status
		}
	}
	if c := m.convs[id]; c != nil && c.CurrentJobID == jobID {
		c.Status = status
		c.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MemoryStore) AppendDelta(_ context.Context, eventID, jobID string, sequence uint64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.deltaEvents[eventID]; ok {
		return nil
	}
	id := m.byJob[jobID]
	if id == "" {
		return ErrNotFound
	}
	for _, turn := range m.turns[id] {
		if turn.jobID == jobID && isTerminal(turn.state) {
			return nil
		}
	}
	m.deltaEvents[eventID] = struct{}{}
	if m.deltas[jobID] == nil {
		m.deltas[jobID] = make(map[uint64]string)
	}
	if _, ok := m.deltas[jobID][sequence]; ok {
		return nil
	}
	m.deltas[jobID][sequence] = text
	full := orderedDelta(m.deltas[jobID])
	for i := range m.messages[id] {
		if m.messages[id][i].JobID == jobID && m.messages[id][i].Role == "assistant" {
			m.messages[id][i].Content = full
		}
	}
	if c := m.convs[id]; c != nil {
		c.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MemoryStore) AppendActivity(_ context.Context, eventID, jobID string, sequence uint64, text string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.activityEvents[eventID]; ok {
		return nil
	}
	id := m.byJob[jobID]
	if id == "" {
		return ErrNotFound
	}
	for _, turn := range m.turns[id] {
		if turn.jobID == jobID && isTerminal(turn.state) {
			return nil
		}
	}
	for _, activity := range m.activities[jobID] {
		if activity.Sequence == sequence {
			return nil
		}
	}
	m.activityEvents[eventID] = struct{}{}
	m.activities[jobID] = append(m.activities[jobID], Activity{
		JobID: jobID, Sequence: sequence, Content: text, CreatedAt: time.Now().UTC(),
	})
	if c := m.convs[id]; c != nil {
		c.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MemoryStore) SetAssistantFinal(_ context.Context, jobID, content, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.byJob[jobID]
	if id == "" {
		return ErrNotFound
	}
	for i := range m.messages[id] {
		if m.messages[id][i].JobID == jobID && m.messages[id][i].Role == "assistant" {
			m.messages[id][i].Content = content
			m.messages[id][i].Status = status
		}
	}
	if c := m.convs[id]; c != nil {
		c.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (m *MemoryStore) Messages(_ context.Context, conversationID string) ([]Message, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.convs[conversationID] == nil {
		return nil, ErrNotFound
	}
	out := append([]Message(nil), m.messages[conversationID]...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].TurnNo == out[j].TurnNo {
			return out[i].Role == "user" && out[j].Role != "user"
		}
		return out[i].TurnNo < out[j].TurnNo
	})
	return out, nil
}

func (m *MemoryStore) Activities(_ context.Context, conversationID, jobID string, limit int) ([]Activity, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.convs[conversationID] == nil {
		return nil, ErrNotFound
	}
	if m.byJob[jobID] != conversationID {
		return nil, nil
	}
	out := append([]Activity(nil), m.activities[jobID]...)
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func orderedDelta(deltas map[uint64]string) string {
	seq := make([]uint64, 0, len(deltas))
	for n := range deltas {
		seq = append(seq, n)
	}
	sort.Slice(seq, func(i, j int) bool { return seq[i] < seq[j] })
	var b []byte
	for _, n := range seq {
		b = append(b, deltas[n]...)
	}
	return string(b)
}

func isTerminal(status string) bool {
	switch status {
	case "SUCCESS", "FAILED", "CANCELLED", "TIMEOUT":
		return true
	default:
		return false
	}
}
