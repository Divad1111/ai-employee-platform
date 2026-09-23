package automation

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// 存储错误。
var (
	ErrNotFound            = errors.New("自动化规则不存在")
	ErrInvalidInput        = errors.New("参数无效")
	ErrIdempotencyConflict = errors.New("idempotency_key 冲突")
	ErrForbiddenWebhook    = errors.New("webhook 校验失败")
	ErrRateLimited         = errors.New("webhook 限流")
)

// Store 持久化接口。
type Store interface {
	Save(ctx context.Context, a *Automation) error
	Get(ctx context.Context, id string) (*Automation, error)
	List(ctx context.Context, triggerType string) ([]*Automation, error)
	Delete(ctx context.Context, id string) error

	SaveCalendarItem(ctx context.Context, item *CalendarItem) error
	ListCalendarItems(ctx context.Context, automationID, runDate string) ([]*CalendarItem, error)
	GetCalendarItem(ctx context.Context, id string) (*CalendarItem, error)
	DeleteCalendarItemsByDate(ctx context.Context, automationID, runDate string) error
	ReplaceCalendarItems(ctx context.Context, automationID, runDate string, items []*CalendarItem) error

	SaveRun(ctx context.Context, r *Run) error
	GetRun(ctx context.Context, id string) (*Run, error)
	GetRunByIdempotency(ctx context.Context, key string) (*Run, error)
	GetRunByJobID(ctx context.Context, jobID string) (*Run, error)
	ListRuns(ctx context.Context, automationID string, limit int) ([]*Run, error)
	HasActiveOrSuccessForItem(ctx context.Context, calendarItemID string) (bool, error)
	NextCalendarItem(ctx context.Context, automationID, runDate string, afterSeq int) (*CalendarItem, error)
	GetByWebhookPathToken(ctx context.Context, pathToken string) (*Automation, error)
}

// MemoryStore 内存实现（测试 / 无 DB 回退）。
type MemoryStore struct {
	mu        sync.RWMutex
	autos     map[string]*Automation
	items     map[string]*CalendarItem
	runs      map[string]*Run
	idemIndex map[string]string // idem → runID
	jobIndex  map[string]string // jobID → runID
}

// NewMemoryStore 创建内存存储。
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		autos:     map[string]*Automation{},
		items:     map[string]*CalendarItem{},
		runs:      map[string]*Run{},
		idemIndex: map[string]string{},
		jobIndex:  map[string]string{},
	}
}

func cloneAuto(a *Automation) *Automation {
	if a == nil {
		return nil
	}
	c := *a
	if a.TriggerConfig != nil {
		c.TriggerConfig = append(json.RawMessage(nil), a.TriggerConfig...)
	}
	if a.LastFiredAt != nil {
		t := *a.LastFiredAt
		c.LastFiredAt = &t
	}
	return &c
}

func cloneItem(i *CalendarItem) *CalendarItem {
	if i == nil {
		return nil
	}
	c := *i
	return &c
}

func cloneRun(r *Run) *Run {
	if r == nil {
		return nil
	}
	c := *r
	if r.Payload != nil {
		c.Payload = append(json.RawMessage(nil), r.Payload...)
	}
	if r.FinishedAt != nil {
		t := *r.FinishedAt
		c.FinishedAt = &t
	}
	return &c
}

func (m *MemoryStore) Save(_ context.Context, a *Automation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autos[a.ID] = cloneAuto(a)
	return nil
}

func (m *MemoryStore) Get(_ context.Context, id string) (*Automation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.autos[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneAuto(a), nil
}

func (m *MemoryStore) List(_ context.Context, triggerType string) ([]*Automation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Automation, 0, len(m.autos))
	for _, a := range m.autos {
		if triggerType != "" && a.TriggerType != triggerType {
			continue
		}
		out = append(out, cloneAuto(a))
	}
	return out, nil
}

func (m *MemoryStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.autos[id]; !ok {
		return ErrNotFound
	}
	delete(m.autos, id)
	for k, it := range m.items {
		if it.AutomationID == id {
			delete(m.items, k)
		}
	}
	return nil
}

func (m *MemoryStore) SaveCalendarItem(_ context.Context, item *CalendarItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items[item.ID] = cloneItem(item)
	return nil
}

func (m *MemoryStore) ListCalendarItems(_ context.Context, automationID, runDate string) ([]*CalendarItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*CalendarItem
	for _, it := range m.items {
		if it.AutomationID != automationID {
			continue
		}
		if runDate != "" && it.RunDate != runDate {
			continue
		}
		out = append(out, cloneItem(it))
	}
	// 按 seq 排序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Seq < out[i].Seq || (out[j].Seq == out[i].Seq && out[j].RunDate < out[i].RunDate) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out, nil
}

func (m *MemoryStore) GetCalendarItem(_ context.Context, id string) (*CalendarItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	it, ok := m.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneItem(it), nil
}

func (m *MemoryStore) DeleteCalendarItemsByDate(_ context.Context, automationID, runDate string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, it := range m.items {
		if it.AutomationID == automationID && it.RunDate == runDate {
			delete(m.items, k)
		}
	}
	return nil
}

func (m *MemoryStore) ReplaceCalendarItems(ctx context.Context, automationID, runDate string, items []*CalendarItem) error {
	if err := m.DeleteCalendarItemsByDate(ctx, automationID, runDate); err != nil {
		return err
	}
	for _, it := range items {
		if err := m.SaveCalendarItem(ctx, it); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryStore) SaveRun(_ context.Context, r *Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runs[r.ID] = cloneRun(r)
	if r.IdempotencyKey != "" {
		m.idemIndex[r.IdempotencyKey] = r.ID
	}
	if r.JobID != "" {
		m.jobIndex[r.JobID] = r.ID
	}
	return nil
}

func (m *MemoryStore) GetRun(_ context.Context, id string) (*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRun(r), nil
}

func (m *MemoryStore) GetRunByIdempotency(_ context.Context, key string) (*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.idemIndex[key]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRun(m.runs[id]), nil
}

func (m *MemoryStore) GetRunByJobID(_ context.Context, jobID string) (*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.jobIndex[jobID]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRun(m.runs[id]), nil
}

func (m *MemoryStore) ListRuns(_ context.Context, automationID string, limit int) ([]*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []*Run
	for _, r := range m.runs {
		if automationID != "" && r.AutomationID != automationID {
			continue
		}
		out = append(out, cloneRun(r))
	}
	// 按 CreatedAt 降序
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].CreatedAt.After(out[i].CreatedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryStore) HasActiveOrSuccessForItem(_ context.Context, calendarItemID string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.runs {
		if r.CalendarItemID != calendarItemID {
			continue
		}
		switch r.Status {
		case RunTriggered, RunJobCreated, RunSuccess:
			return true, nil
		}
	}
	return false, nil
}

func (m *MemoryStore) NextCalendarItem(_ context.Context, automationID, runDate string, afterSeq int) (*CalendarItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var best *CalendarItem
	for _, it := range m.items {
		if it.AutomationID != automationID || it.RunDate != runDate || !it.Enabled {
			continue
		}
		if it.Seq <= afterSeq {
			continue
		}
		if best == nil || it.Seq < best.Seq {
			c := cloneItem(it)
			best = c
		}
	}
	return best, nil
}

func (m *MemoryStore) GetByWebhookPathToken(_ context.Context, pathToken string) (*Automation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, a := range m.autos {
		if a.TriggerType != TriggerWebhook {
			continue
		}
		var cfg WebhookConfig
		_ = json.Unmarshal(a.TriggerConfig, &cfg)
		if cfg.PathToken == pathToken {
			return cloneAuto(a), nil
		}
	}
	return nil, ErrNotFound
}

// 确保 MemoryStore 在测试中时间字段可用。
var _ Store = (*MemoryStore)(nil)
