package automation

import (
	"context"
	"fmt"

	"github.com/ai-employee-platform/server/internal/feishu"
)

// Auditor 审计接口（与 job 包约定一致）。
type Auditor interface {
	Log(ctx context.Context, actorType, actorID, action, result, ip string, meta map[string]string)
}

// FeishuNotifier 飞书发送能力。
type FeishuNotifier interface {
	NotifyJobResult(ctx context.Context, chatID, jobID, status, summary string) error
	SendText(ctx context.Context, chatID, text string) error
	ResolveTargetChat(employeeID string) string
}

// ChatBinder 绑定 Job → 飞书 chat（复用 notification.RememberChat）。
type ChatBinder interface {
	RememberChat(jobID, chatID string)
}

// FeishuBridge 将 feishu.Service 适配为 FeishuNotifier。
type FeishuBridge struct {
	Svc *feishu.Service
}

func (a FeishuBridge) NotifyJobResult(ctx context.Context, chatID, jobID, status, summary string) error {
	if a.Svc == nil {
		return nil
	}
	return a.Svc.NotifyJobResult(ctx, chatID, jobID, status, summary)
}

func (a FeishuBridge) SendText(ctx context.Context, chatID, text string) error {
	if a.Svc == nil || chatID == "" {
		return nil
	}
	return a.Svc.NotifyJobResult(ctx, chatID, "AUTOMATION", "RUNNING", text)
}

func (a FeishuBridge) ResolveTargetChat(employeeID string) string {
	if a.Svc == nil {
		return ""
	}
	return a.Svc.ResolveTargetChat(employeeID)
}

// notifyEvent 写 Audit，并在有 chat 时发飞书。
func (s *Service) notifyEvent(ctx context.Context, action, result, actorID, ip, chatID string, meta map[string]string, feishuText string) {
	if s.Audit != nil {
		s.Audit.Log(ctx, "SYSTEM", actorID, action, result, ip, meta)
	}
	if chatID == "" || feishuText == "" || s.Feishu == nil {
		return
	}
	_ = s.Feishu.SendText(ctx, chatID, feishuText)
}

func buildTriggerText(name, source string) string {
	return fmt.Sprintf("【自动化触发】规则「%s」已触发（来源：%s）", name, source)
}

func buildJobCreatedText(name, jobID, employeeID string) string {
	return fmt.Sprintf("【自动化建单】规则「%s」已创建 Job `%s`（员工：%s）", name, jobID, employeeID)
}

func buildTerminalText(name, jobID, status, summary string, chainStopped bool) string {
	msg := fmt.Sprintf("【自动化结果】规则「%s」Job `%s` → %s", name, jobID, status)
	if summary != "" {
		msg += "\n" + summary
	}
	if chainStopped {
		msg += "\n⚠️ 日历链式任务已中断，后续条目不会自动执行。"
	}
	return msg
}
