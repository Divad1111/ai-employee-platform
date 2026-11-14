package taskconversation

import (
	"context"
	"strings"

	"github.com/ai-employee-platform/server/internal/job"
)

// loadCardJobState 从任务本身恢复调度进度和失败原因，兼容重启及先于卡片发生的事件。
// 原因只用于状态说明，不能混入 Agent 回复或后续任务的会话上下文。
func (s *Service) loadCardJobState(ctx context.Context, c *Conversation) (*job.Job, error) {
	if s.Jobs == nil || c.CurrentJobID == "" {
		return nil, nil
	}
	j, err := s.Jobs.Get(ctx, c.CurrentJobID)
	if err != nil {
		return nil, err
	}
	// 终态回调已到达时，不因并发读取到较早状态而把卡片退回排队中。
	if !isTerminal(c.Status) || isTerminal(j.Status) {
		c.Status = j.Status
	}
	c.StatusReason = ""
	waiting := c.Status == job.StatusCreated || c.Status == job.StatusQueued
	failed := c.Status == job.StatusFailed || c.Status == job.StatusTimeout
	if !waiting && !failed {
		return j, nil
	}
	events, err := s.Jobs.ListEvents(ctx, j.ID)
	if err != nil {
		return nil, err
	}
	for i := len(events) - 1; i >= 0; i-- {
		event := events[i]
		if event == nil {
			continue
		}
		if waiting && event.EventType != "SCHEDULING_BLOCKED" {
			continue
		}
		if failed && (event.EventType != "STATUS" || event.Payload["to"] != c.Status) {
			continue
		}
		c.StatusReason = strings.TrimSpace(event.Payload["error"])
		if c.StatusReason == "" {
			c.StatusReason = strings.TrimSpace(event.Payload["reason"])
		}
		break
	}
	return j, nil
}
