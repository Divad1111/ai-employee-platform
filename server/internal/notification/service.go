// Package notification 统一通知（飞书/Admin SSE）。
// 设计依据：设计文档 §109。
package notification

import (
	"context"
	"strings"
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

	mu        sync.Mutex
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

// GetChat 获取 Job 对应的飞书会话。
func (s *Service) GetChat(jobID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chatByJob[jobID]
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
	if chat == "" && s.Feishu != nil && j.EmployeeID != "" {
		chat = s.Feishu.ResolveTargetChat(j.EmployeeID)
	}
	summary := notifySummary(j.Status, j.Result, s.latestError(ctx, j))
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

// latestError 取终态时间线上最近一条 error。失败通知不能只看 reply。
func (s *Service) latestError(ctx context.Context, j *job.Job) string {
	if j == nil || j.Status != job.StatusFailed || s.Jobs == nil {
		return ""
	}
	events, err := s.Jobs.ListEvents(ctx, j.ID)
	if err != nil {
		return ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i] == nil || events[i].Payload == nil {
			continue
		}
		if msg := strings.TrimSpace(events[i].Payload["error"]); msg != "" {
			return msg
		}
		if msg := strings.TrimSpace(events[i].Payload["reason"]); msg != "" {
			return msg
		}
	}
	return ""
}

// notifySummary 成功只用回复；失败时无论回复是否为空，都带上 error。
func notifySummary(status, reply, errMsg string) string {
	reply = strings.TrimSpace(reply)
	errMsg = strings.TrimSpace(errMsg)
	if status != job.StatusFailed {
		if reply == "" {
			return "status=" + status
		}
		return reply
	}
	switch {
	case errMsg == "" && reply == "":
		return "status=FAILED"
	case errMsg == "":
		return reply
	case reply == "" || reply == errMsg:
		return errMsg
	default:
		return reply + "\n\n" + errMsg
	}
}
