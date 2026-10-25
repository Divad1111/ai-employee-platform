package feishu

import (
	"context"
	"errors"
	"fmt"
	input "github.com/ai-employee-platform/gen/jobinput"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/message"
	"github.com/ai-employee-platform/server/internal/quota"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/workstation"
)

// ChatBinder 记录 Job 与飞书会话（由 notification 实现，避免包循环）。
type ChatBinder interface {
	RememberChat(jobID, chatID string)
}

// UserRoles 查询平台用户角色，供配额检查。
type UserRoles func(ctx context.Context, userID string) []string

// WorkspaceResolver 把员工上的工作区引用解析成已登记 id（路径会登记为工作区）。
type WorkspaceResolver interface {
	ResolveBinding(ctx context.Context, ref, workstationID, employeeID, actorID, ip string) (string, error)
}

// Bridge 将飞书接入接到 Employee/Job/Message/Scheduler/Notification。
type Bridge struct {
	AuthorizeUser func(context.Context, string) error
	Employees     *employee.Service
	Jobs          *job.Service
	Messages      *message.Service
	Scheduler     *scheduler.Service
	Notify        ChatBinder
	Feishu        *Service
	Quota         *quota.Service
	Roles         UserRoles
	Workspaces    WorkspaceResolver
	Workstations  *workstation.Service
	Conversations TaskConversationStarter
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

// GetEmployeeName 实现 EmployeeChecker：获取员工真实名称
func (b *Bridge) GetEmployeeName(ctx context.Context, employeeID string) string {
	if b.Employees == nil || employeeID == "" {
		return ""
	}
	e, err := b.Employees.Get(ctx, employeeID)
	if err != nil || e == nil {
		return ""
	}
	return e.Name
}

// CreateFromFeishu 实现 JobCreator：写 Message → 建 Job → 尝试调度。
func (b *Bridge) CreateFromFeishu(ctx context.Context, employeeID, prompt, idempotencyKey, chatID, messageID, senderOpenID string) (string, error) {
	return b.CreateFromFeishuWithAttachments(ctx, employeeID, prompt, idempotencyKey, chatID, messageID, senderOpenID, nil)
}

func (b *Bridge) CreateFromFeishuWithAttachments(ctx context.Context, employeeID, prompt, idempotencyKey, chatID, messageID, senderOpenID string, files []input.File) (string, error) {
	if b.Feishu == nil || !b.Feishu.EmployeeHasOpenID(employeeID, senderOpenID) || b.Feishu.BindingByEmployee(employeeID) == nil || !b.Feishu.BindingByEmployee(employeeID).Verified {
		return "", errors.New("无权使用该数字员工")
	}

	e, err := b.Employees.Get(ctx, employeeID)
	if err != nil {
		return "", err
	}
	binding := b.Feishu.BindingByEmployee(employeeID)
	if binding == nil || binding.OwnerUserID != e.OwnerUserID {
		return "", errors.New("员工归属已变更，请重新确认飞书绑定")
	}
	userID := b.feishuUserID(ctx, senderOpenID, e)
	if userID == "" {
		return "", errors.New("该飞书账号未绑定平台用户，无法计入配额")
	}
	if b.AuthorizeUser == nil {
		return "", errors.New("用户授权不可用")
	}
	if err := b.AuthorizeUser(ctx, userID); err != nil {
		return "", err
	}
	_, _ = b.Messages.Send(ctx, message.SendInput{
		SenderType: message.TypeUser, SenderID: "feishu:" + messageID,
		ReceiverType: message.TypeEmployee, ReceiverID: employeeID,
		Content: prompt,
	}, "feishu", "")

	if b.Quota != nil {
		var roles []string
		if b.Roles != nil {
			roles = b.Roles(ctx, userID)
		}
		if err := b.Quota.CheckUser(ctx, userID, roles, 1, 1); err != nil {
			if errors.Is(err, quota.ErrExceeded) {
				return "", errors.New("本月 Token 配额已用尽，无法创建新任务")
			}
			return "", err
		}
	}
	clientIP := senderOpenID
	if clientIP == "" && b.Feishu != nil {
		if bnd := b.Feishu.BindingByEmployee(employeeID); bnd != nil {
			clientIP = bnd.FeishuOpenID
		}
	}
	wsID := e.WorkspaceID
	if b.Workspaces != nil && wsID != "" {
		resolved, rerr := b.Workspaces.ResolveBinding(ctx, wsID, e.WorkstationID, employeeID, userID, clientIP)
		if rerr != nil {
			return "", rerr
		}
		if resolved != wsID {
			if _, uerr := b.Employees.Update(ctx, employeeID, employee.UpdateInput{WorkspaceID: &resolved}, userID, clientIP); uerr != nil {
				return "", uerr
			}
			wsID = resolved
		}
	}
	j, _, err := b.Jobs.Create(ctx, job.CreateInput{
		EmployeeID:       employeeID,
		WorkspaceID:      wsID,
		WorkstationID:    e.WorkstationID,
		Prompt:           prompt,
		InputAttachments: files,
		IdempotencyKey:   idempotencyKey,
		TimeoutSec:       600,
		CreatedBy:        userID,
		Source:           job.SourceFeishu,
	}, userID, clientIP)
	if err != nil {
		return "", err
	}
	notifyTarget := chatID
	if notifyTarget == "" && b.Feishu != nil {
		notifyTarget = b.Feishu.ResolveTargetChat(employeeID)
	}
	if b.Notify != nil && notifyTarget != "" {
		b.Notify.RememberChat(j.ID, notifyTarget)
	}
	if b.Conversations != nil {
		if _, err := b.Conversations.Start(ctx, TaskConversationStart{
			JobID: j.ID, EmployeeID: employeeID, EmployeeName: e.Name,
			OwnerUserID: userID, OwnerOpenID: senderOpenID, ChatID: notifyTarget, Prompt: prompt,
			SessionGeneration: e.SessionGeneration,
		}); err != nil {
			fmt.Printf("[Feishu] 建立任务会话卡片失败 (job=%s): %v\n", j.ID, err)
		}
	}
	if b.Scheduler != nil {
		_, _ = b.Scheduler.ScheduleJob(ctx, j.ID)
	}
	return j.ID, nil
}

// feishuUserID 用飞书发送者 OpenID 匹配绑定，再取该数字员工的归属用户。
func (b *Bridge) feishuUserID(ctx context.Context, senderOpenID string, target *employee.Employee) string {
	if b.Feishu == nil || b.Employees == nil || senderOpenID == "" {
		return ""
	}
	if target != nil && target.OwnerUserID != "" && b.Feishu.EmployeeHasOpenID(target.ID, senderOpenID) {
		return target.OwnerUserID
	}
	return ""
}

// Wire 把 Bridge 挂到 Feishu Service。
func (b *Bridge) Wire() {
	b.Feishu.Employees = b
	b.Feishu.Jobs = b
	b.Feishu.Commands = b
	b.Feishu.AuthorizeActor = func(ctx context.Context, id, actor string) error {
		e, err := b.Employees.Get(ctx, id)
		if err != nil || e == nil {
			return ErrNoEmployee
		}
		binding := b.Feishu.BindingByEmployee(id)
		if binding == nil || !binding.Verified || binding.OwnerUserID != e.OwnerUserID || binding.FeishuOpenID != actor {
			return errors.New("飞书身份未绑定或员工归属已变更")
		}
		if b.AuthorizeUser == nil {
			return errors.New("平台用户授权不可用")
		}
		return b.AuthorizeUser(ctx, e.OwnerUserID)
	}
}
