package scheduler

import (
	"errors"
	"fmt"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
)

// Only normal queue conditions are retryable; actual dispatch errors are terminal.
func isSchedulingWait(err error) bool {
	return errors.Is(err, ErrEmployeeBusy) || errors.Is(err, ErrConversationQueued) ||
		errors.Is(err, ErrNoWorkstation) || errors.Is(err, ErrResourceUnavailable)
}

func (s *Service) abortStartCommand(cmd *aiev1.Command, reason string) {
	type aborter interface {
		AbortJobStart(wsID, employeeID, jobID, reason string) error
	}
	var err error
	if p, ok := s.Pusher.(aborter); ok {
		err = p.AbortJobStart(cmd.GetWorkstationId(), cmd.GetEmployeeId(), cmd.GetJobId(), reason)
	} else if p, ok := s.FullPusher.(aborter); ok {
		err = p.AbortJobStart(cmd.GetWorkstationId(), cmd.GetEmployeeId(), cmd.GetJobId(), reason)
	} else if s.Pusher != nil {
		_, err = s.Pusher.PushCommand(cmd.GetWorkstationId(), aiev1.CommandType_COMMAND_TYPE_STOP_JOB, cmd.GetEmployeeId(), cmd.GetJobId(), `{"reason":"scheduling_failed"}`)
	}
	if err != nil {
		fmt.Printf("[Scheduler] 停止调度失败任务 %s 失败（已记录失败状态）: %v\n", cmd.GetJobId(), err)
	}
}
