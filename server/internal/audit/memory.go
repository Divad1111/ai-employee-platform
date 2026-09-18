// Package audit 提供结构化审计日志（与普通日志分离）。
// 设计依据：设计文档 §31、§86。
package audit

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"
)

// Entry 单条审计记录。
type Entry struct {
	ID         int64             `json:"id"`
	ActorType  string            `json:"actor_type"`
	ActorID    string            `json:"actor_id"`
	Action     string            `json:"action"`
	TargetType string            `json:"target_type,omitempty"`
	TargetID   string            `json:"target_id,omitempty"`
	Result     string            `json:"result"`
	IP         string            `json:"ip"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// Memory 内存审计存储（开发/测试；生产应写入 PostgreSQL）。
type Memory struct {
	mu      sync.Mutex
	entries []Entry
	nextID  int64
}

// NewMemory 创建内存审计器。
func NewMemory() *Memory {
	return &Memory{}
}

// Log 追加审计记录。
func (m *Memory) Log(_ context.Context, actorType, actorID, action, result, ip string, meta map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cp := map[string]string{}
	for k, v := range meta {
		cp[k] = v
	}
	targetType, targetID := "", ""
	if meta != nil {
		if t := meta["target_type"]; t != "" {
			targetType = t
		}
		if t := meta["target_id"]; t != "" {
			targetID = t
		}
		if targetID == "" {
			if sid := meta["secret_id"]; sid != "" {
				targetType, targetID = "secret", sid
			}
			if aid := meta["approval_id"]; aid != "" {
				targetType, targetID = "approval", aid
			}
		}
	}
	m.entries = append(m.entries, Entry{
		ID:         m.nextID,
		ActorType:  actorType,
		ActorID:    actorID,
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Result:     result,
		IP:         ip,
		Metadata:   cp,
		CreatedAt:  time.Now().UTC(),
	})
}

// Filter 审计查询条件。
type Filter struct {
	Actor      string
	Action     string
	ActionPrefix string
	TargetType string
	TargetID   string
	Limit      int
}

// Query 按条件过滤（新→旧）。
func (m *Memory) Query(f Filter) []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	var matched []Entry
	for i := len(m.entries) - 1; i >= 0; i-- {
		e := m.entries[i]
		if f.Actor != "" && e.ActorID != f.Actor && e.ActorType != f.Actor {
			continue
		}
		if f.Action != "" && e.Action != f.Action {
			continue
		}
		if f.ActionPrefix != "" && !strings.HasPrefix(e.Action, f.ActionPrefix) {
			continue
		}
		if f.TargetType != "" && e.TargetType != f.TargetType {
			continue
		}
		if f.TargetID != "" && e.TargetID != f.TargetID {
			continue
		}
		matched = append(matched, e)
		if f.Limit > 0 && len(matched) >= f.Limit {
			break
		}
	}
	return matched
}

// ExportJSON 导出（与 Application Log 分离；不可由普通 API 删除）。
func (m *Memory) ExportJSON(f Filter) ([]byte, error) {
	items := m.Query(f)
	return json.MarshalIndent(map[string]any{"items": items, "exported_at": time.Now().UTC()}, "", "  ")
}

// Count 条目数。
func (m *Memory) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// ArchiveBefore 仅超级管理员归档策略：移除早于 cutoff 的记录（不可被普通用户调用）。
func (m *Memory) ArchiveBefore(cutoff time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var kept []Entry
	removed := 0
	for _, e := range m.entries {
		if e.CreatedAt.Before(cutoff) {
			removed++
			continue
		}
		kept = append(kept, e)
	}
	m.entries = kept
	return removed
}

// List 返回最近 n 条（新→旧）。
func (m *Memory) List(n int) []Entry {
	return m.Query(Filter{Limit: n})
}
