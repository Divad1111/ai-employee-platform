package scheduler

import (
	"context"
	"strings"

	"github.com/ai-employee-platform/server/internal/job"
)

// Recover assignments that failed before a start command could be delivered.
// Serialize with dispatch so a slow, still-active assignment is never reclaimed.
func (s *Service) failAbandonedAssignment(ctx context.Context, id string) {
	s.scheduleMu.Lock()
	var failedJob *job.Job
	defer func() {
		s.scheduleMu.Unlock()
		if failedJob != nil && s.OnTerminal != nil {
			s.OnTerminal(ctx, failedJob)
		}
	}()
	j, err := s.Jobs.Get(ctx, id)
	if err != nil || j.Status != job.StatusAssigned || j.SessionID != "" || !j.StartedAt.IsZero() || s.startStillPending(j.WorkstationID, j.ID) {
		return
	}
	events, err := s.Jobs.ListEvents(ctx, id)
	if err != nil {
		return
	}
	var dispatchError string
	for _, event := range events {
		if event.EventType == "SCHEDULING_BLOCKED" && event.Payload["status"] == job.StatusAssigned {
			dispatchError = event.Payload["reason"]
		}
	}
	// Legacy context lookup failures occurred before command enqueueing. Do not
	// reclaim ambiguous send errors: those commands may already be executing.
	if !strings.HasPrefix(dispatchError, "读取员工会话上下文失败:") {
		return
	}
	reason := "任务分配后下发失败，已释放员工执行占用：" + dispatchError
	updated, err := s.Jobs.Transition(ctx, id, job.StatusFailed, "scheduler", "", map[string]string{"reason": reason, "error": reason})
	if err == nil {
		failedJob = updated
	}
}
