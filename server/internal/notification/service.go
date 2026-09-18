// Package notification 统一通知（飞书/Admin SSE）。
// 设计依据：设计文档 §109。
package notification

import (
	"context"
	"sync"

	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
)

// Service 订阅 Job 终态并通知。
type Service struct {
	Feishu *feishu.Service
	Bus    *eventbus.Bus
	Jobs   *job.Service

	mu       sync.Mutex
	chatByJob map[string]string // job_id → feishu chat_id
}

// New 创建。
func New(fs *feishu.Service, bus *eventbus.Bus, jobs *job.Service) *Service {
	return &Service{
		Feishu: fs, Bus: bus, Jobs: jobs,
		chatByJob: map[string]string{},
	}
}

// RememberChat 记录 Job 对应飞书会话。
func (s *Service) RememberChat(jobID, chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chatByJob[jobID] = chatID
}

// OnJobTerminal Job 终态回调。
func (s *Service) OnJobTerminal(ctx context.Context, j *job.Job) error {
	if j == nil {
		return nil
	}
	if j.Status != job.StatusSuccess && j.Status != job.StatusFailed &&
		j.Status != job.StatusCancelled && j.Status != job.StatusTimeout {
		return nil
	}
	s.mu.Lock()
	chat := s.chatByJob[j.ID]
	s.mu.Unlock()
	summary := j.Result
	if summary == "" {
		summary = "status=" + j.Status
	}
	if s.Bus != nil {
		s.Bus.Publish(ctx, eventbus.TypeJobStatus, map[string]string{
			"job_id": j.ID, "status": j.Status,
		})
	}
	if s.Feishu != nil {
		return s.Feishu.NotifyJobResult(ctx, chat, j.ID, j.Status, summary)
	}
	return nil
}
