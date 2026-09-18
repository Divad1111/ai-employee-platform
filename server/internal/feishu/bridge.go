package feishu

import (
	"context"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/scheduler"
)

// ChatBinder 记录 Job 与飞书会话（由 notification 实现，避免包循环）。
type ChatBinder interface {
	RememberChat(jobID, chatID string)
}

// Bridge 将飞书接入接到 Employee/Job/Message/Scheduler/Notification。
type Bridge struct {
	Employees *employee.Service
	Jobs      *job.Service
	Messages  *message.Service
	Scheduler *scheduler.Service
	Notify    ChatBinder
	Feishu    *Service
}

// ResolveAlias 实现 EmployeeChecker。
func (b *Bridge) ResolveAlias(ctx context.Context, alias string) (string, error) {
	_ = ctx
	_ = alias
	return "", ErrNoEmployee
}

// IsAssignable Employee 启用且绑定 Workstation（V1 简化 Permission）。
func (b *Bridge) IsAssignable(ctx context.Context, employeeID string) error {
	e, err := b.Employees.Get(ctx, employeeID)
	if err != nil {
		return err
	}
	if e.Status == employee.StatusDisabled {
		return ErrDisabled
	}
	if e.WorkstationID == "" {
		return ErrDisabled
	}
	return nil
}

// CreateFromFeishu 实现 JobCreator：写 Message → 建 Job → 尝试调度。
func (b *Bridge) CreateFromFeishu(ctx context.Context, employeeID, prompt, idempotencyKey, chatID, messageID string) (string, error) {
	_, _ = b.Messages.Send(ctx, message.SendInput{
		SenderType: message.TypeUser, SenderID: "feishu:" + messageID,
		ReceiverType: message.TypeEmployee, ReceiverID: employeeID,
		Content: prompt,
	}, "feishu", "")

	e, err := b.Employees.Get(ctx, employeeID)
	if err != nil {
		return "", err
	}
	j, _, err := b.Jobs.Create(ctx, job.CreateInput{
		EmployeeID:     employeeID,
		WorkspaceID:    e.WorkspaceID,
		WorkstationID:  e.WorkstationID,
		Prompt:         prompt,
		IdempotencyKey: idempotencyKey,
		TimeoutSec:     600,
		CreatedBy:      "feishu",
	}, "feishu", "")
	if err != nil {
		return "", err
	}
	if b.Notify != nil && chatID != "" {
		b.Notify.RememberChat(j.ID, chatID)
	}
	if b.Scheduler != nil {
		_, _ = b.Scheduler.ScheduleJob(ctx, j.ID)
	}
	return j.ID, nil
}

// Wire 把 Bridge 挂到 Feishu Service。
func (b *Bridge) Wire() {
	b.Feishu.Employees = b
	b.Feishu.Jobs = b
}
