// Package inquiry 提供工作站 AI Agent 询问/决策向飞书通知与交互全链路处理。
package inquiry

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// InquiryOption 选项结构
type InquiryOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
}

// Inquiry 内存中的待办询问实例
type Inquiry struct {
	ID                 string          `json:"id"`
	JobID              string          `json:"job_id"`
	WorkstationID      string          `json:"workstation_id"`
	EmployeeID         string          `json:"employee_id"`
	ChatID             string          `json:"chat_id"`
	Message            string          `json:"message"`
	Options            []InquiryOption `json:"options"`
	Status             string          `json:"status"` // PENDING | RESOLVED | EXPIRED
	SelectedOptionID   string          `json:"selected_option_id,omitempty"`
	SelectedOptionName string          `json:"selected_option_name,omitempty"`
	ResolvedBy         string          `json:"resolved_by,omitempty"`
	ResolvedVia        string          `json:"resolved_via,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	ResolvedAt         time.Time       `json:"resolved_at,omitempty"`
}

// CommandPusher 下发控制平面指令至工作站
type CommandPusher interface {
	PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error)
}

// JobManager 任务状态与时间线操作
type JobManager interface {
	Get(ctx context.Context, id string) (*job.Job, error)
	Transition(ctx context.Context, id, to, actorID, ip string, meta map[string]string) (*job.Job, error)
	AppendEvent(ctx context.Context, jobID, eventType string, payload map[string]string) error
}

// EmployeeGetter 查询数字员工信息
type EmployeeGetter interface {
	Get(ctx context.Context, id string) (*employee.Employee, error)
}

// ChatResolver 获取任务关联的飞书会话
type ChatResolver interface {
	GetChat(jobID string) string
}

// Service 协调 AI Agent 询问交互服务
type Service struct {
	mu        sync.RWMutex
	inquiries map[string]*Inquiry // inquiryID -> Inquiry
	byChat    map[string]*Inquiry // chatID -> latest PENDING Inquiry
	byJob     map[string]*Inquiry // jobID -> latest PENDING Inquiry

	commands     CommandPusher
	jobs         JobManager
	employees    EmployeeGetter
	feishu       *feishu.Service
	chatResolver ChatResolver
}

// NewService 创建 Inquiry 服务
func NewService(cmd CommandPusher, jm JobManager, eg EmployeeGetter, fs *feishu.Service, cr ChatResolver) *Service {
	return &Service{
		inquiries:    make(map[string]*Inquiry),
		byChat:       make(map[string]*Inquiry),
		byJob:        make(map[string]*Inquiry),
		commands:     cmd,
		jobs:         jm,
		employees:    eg,
		feishu:       fs,
		chatResolver: cr,
	}
}

// HandleInquiryEvent 处理工作站上报的 EVENT_TYPE_JOB_INQUIRY 事件
func (s *Service) HandleInquiryEvent(ctx context.Context, wsID string, ev *aiev1.Event) {
	if ev == nil {
		return
	}
	var payload struct {
		InquiryID  string          `json:"inquiry_id"`
		JobID      string          `json:"job_id"`
		EmployeeID string          `json:"employee_id"`
		Message    string          `json:"message"`
		Options    []InquiryOption `json:"options"`
	}
	if pJSON := ev.GetPayloadJson(); pJSON != "" {
		_ = json.Unmarshal([]byte(pJSON), &payload)
	}

	jobID := payload.JobID
	if jobID == "" {
		jobID = ev.GetJobId()
	}
	empID := payload.EmployeeID
	if empID == "" {
		empID = ev.GetEmployeeId()
	}
	inquiryID := payload.InquiryID
	if inquiryID == "" {
		inquiryID = fmt.Sprintf("INQ-%s-%d", jobID, time.Now().UnixNano())
	}
	msgText := payload.Message
	if msgText == "" {
		msgText = "AI Agent 请求确认操作/执行策略"
	}
	options := payload.Options
	if len(options) == 0 {
		options = []InquiryOption{
			{OptionID: "allow-once", Name: "允许本次执行"},
			{OptionID: "deny", Name: "拒绝执行"},
		}
	}

	// 查找该任务对应的飞书会话
	chatID := ""
	if s.chatResolver != nil && jobID != "" {
		chatID = s.chatResolver.GetChat(jobID)
	}
	if chatID == "" && s.feishu != nil && empID != "" {
		chatID = s.feishu.ResolveTargetChat(empID)
	}

	empName := empID
	if s.employees != nil && empID != "" {
		if e, err := s.employees.Get(ctx, empID); err == nil && e != nil && e.Name != "" {
			empName = e.Name
		}
	}

	inq := &Inquiry{
		ID:            inquiryID,
		JobID:         jobID,
		WorkstationID: wsID,
		EmployeeID:    empID,
		ChatID:        chatID,
		Message:       msgText,
		Options:       options,
		Status:        "PENDING",
		CreatedAt:     time.Now().UTC(),
	}

	s.mu.Lock()
	s.inquiries[inquiryID] = inq
	if chatID != "" {
		s.byChat[chatID] = inq
	}
	if jobID != "" {
		s.byJob[jobID] = inq
	}
	s.mu.Unlock()

	// 任务状态机切为 WAITING_APPROVAL
	if s.jobs != nil && jobID != "" {
		_, _ = s.jobs.Transition(ctx, jobID, job.StatusWaitingApproval, "workstation_inquiry", wsID, map[string]string{
			"inquiry_id": inquiryID,
		})
		_ = s.jobs.AppendEvent(ctx, jobID, "INQUIRY_CREATED", map[string]string{
			"inquiry_id": inquiryID,
			"message":    msgText,
		})
	}

	// 发送带交互按钮的飞书卡片消息
	if s.feishu != nil && chatID != "" {
		cardOpts := make([]feishu.InquiryCardOption, len(options))
		for i, o := range options {
			cardOpts[i] = feishu.InquiryCardOption{OptionID: o.OptionID, Name: o.Name}
		}
		card := feishu.BuildInquiryCard(jobID, empName, inquiryID, msgText, cardOpts)
		var err error
		if s.feishu.Sender != nil {
			err = s.feishu.Sender.Send(ctx, feishu.Reply{ChatID: chatID, Content: card.MustJSON()})
		} else {
			_, err = s.feishu.SendCard(ctx, "", chatID, card)
		}
		if err != nil {
			fmt.Printf("[Inquiry] ⚠️ 发送飞书选项卡片失败 (chat=%s): %v\n", chatID, err)
		} else {
			fmt.Printf("[Inquiry] 🔔 成功向飞书发送决策请示卡片: inq=%s, job=%s, chat=%s\n", inquiryID, jobID, chatID)
		}
	}
}

// HandleCardAction 处理飞书卡片按钮点击回调
func (s *Service) HandleCardAction(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}

	val := event.Event.Action.Value
	if val == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}
	act, _ := val["action"].(string)
	if act != "resolve_inquiry" {
		return &callback.CardActionTriggerResponse{}, nil
	}

	inquiryID, _ := val["inquiry_id"].(string)
	optionID, _ := val["option_id"].(string)
	optionName, _ := val["option_name"].(string)

	openID := ""
	if event.Event.Operator != nil {
		openID = event.Event.Operator.OpenID
	}

	inq, err := s.ResolveInquiry(ctx, inquiryID, optionID, optionName, openID, "飞书卡片按钮点击")
	if err != nil {
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{
				Type:    "warning",
				Content: fmt.Sprintf("处理失败：%v", err),
			},
		}, nil
	}

	// 告知飞书客户端成功提示
	toastText := fmt.Sprintf("已成功选择【%s】，已指令工作站继续执行！", inq.SelectedOptionName)
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{
			Type:    "success",
			Content: toastText,
		},
	}, nil
}

// CheckChatReply 检查用户在飞书会话中直接回复的内容是否匹配待确认选项
func (s *Service) CheckChatReply(ctx context.Context, chatID, text, senderOpenID string) (bool, error) {
	chatID = strings.TrimSpace(chatID)
	text = strings.TrimSpace(text)
	if text == "" {
		return false, nil
	}

	s.mu.RLock()
	inq := s.byChat[chatID]
	if inq == nil && senderOpenID != "" {
		inq = s.byChat[senderOpenID]
	}
	s.mu.RUnlock()

	if inq == nil || inq.Status != "PENDING" {
		return false, nil
	}

	// 匹配选项
	matchedOpt, ok := matchOption(text, inq.Options)
	if !ok {
		return false, nil
	}

	_, err := s.ResolveInquiry(ctx, inq.ID, matchedOpt.OptionID, matchedOpt.Name, senderOpenID, "飞书会话直接回复")
	if err != nil {
		return false, err
	}

	// 在会话中回复确认反馈
	if s.feishu != nil && chatID != "" {
		replyText := fmt.Sprintf("✅ 收到回复确认：已选择【%s】，已指令工作站继续执行任务。", matchedOpt.Name)
		if s.feishu.Sender != nil {
			_ = s.feishu.Sender.Send(ctx, feishu.Reply{ChatID: chatID, Content: replyText})
		} else {
			_, _ = s.feishu.SendMessage(ctx, "", chatID, replyText)
		}
	}

	return true, nil
}

// ResolveInquiry 执行选项确认并下发指令给工作站恢复执行
func (s *Service) ResolveInquiry(ctx context.Context, inquiryID, optionID, optionName, actor, via string) (*Inquiry, error) {
	s.mu.Lock()
	inq, ok := s.inquiries[inquiryID]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("请示 %s 不存在或已失效", inquiryID)
	}
	if inq.Status != "PENDING" {
		s.mu.Unlock()
		return inq, fmt.Errorf("请示 %s 已于 %s 完成处理 (状态=%s)", inquiryID, inq.ResolvedAt.Format("15:04:05"), inq.Status)
	}

	if optionName == "" {
		for _, o := range inq.Options {
			if o.OptionID == optionID {
				optionName = o.Name
				break
			}
		}
	}
	if optionName == "" {
		optionName = optionID
	}

	inq.Status = "RESOLVED"
	inq.SelectedOptionID = optionID
	inq.SelectedOptionName = optionName
	inq.ResolvedBy = actor
	inq.ResolvedVia = via
	inq.ResolvedAt = time.Now().UTC()

	if inq.ChatID != "" && s.byChat[inq.ChatID] == inq {
		delete(s.byChat, inq.ChatID)
	}
	if inq.JobID != "" && s.byJob[inq.JobID] == inq {
		delete(s.byJob, inq.JobID)
	}
	s.mu.Unlock()

	// 1. 向工作站推送 COMMAND_TYPE_RESOLVE_INQUIRY 指令
	if s.commands != nil && inq.WorkstationID != "" {
		payload, _ := json.Marshal(map[string]any{
			"inquiry_id":           inq.ID,
			"selected_option_id":   optionID,
			"selected_option_name": optionName,
			"resolved_by":          actor,
			"resolved_via":         via,
		})
		_, err := s.commands.PushCommand(inq.WorkstationID, aiev1.CommandType_COMMAND_TYPE_RESOLVE_INQUIRY, inq.EmployeeID, inq.JobID, string(payload))
		if err != nil {
			fmt.Printf("[Inquiry] ⚠️ 下发选项指令至工作站失败 (ws=%s, inq=%s): %v\n", inq.WorkstationID, inq.ID, err)
		} else {
			fmt.Printf("[Inquiry] 🚀 成功下发选项指令至工作站: ws=%s, inq=%s, option=%s(%s)\n", inq.WorkstationID, inq.ID, optionName, optionID)
		}
	}

	// 2. 任务状态机恢复为 RUNNING
	if s.jobs != nil && inq.JobID != "" {
		_, _ = s.jobs.Transition(ctx, inq.JobID, job.StatusRunning, "inquiry_resolved", actor, map[string]string{
			"inquiry_id":  inq.ID,
			"option_id":   optionID,
			"option_name": optionName,
			"via":         via,
		})
		_ = s.jobs.AppendEvent(ctx, inq.JobID, "INQUIRY_RESOLVED", map[string]string{
			"inquiry_id":  inq.ID,
			"option_id":   optionID,
			"option_name": optionName,
			"by":          actor,
			"via":         via,
		})
	}

	return inq, nil
}

// GetInquiry 获取指定 Inquiry
func (s *Service) GetInquiry(id string) *Inquiry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if inq, ok := s.inquiries[id]; ok {
		cp := *inq
		return &cp
	}
	return nil
}

// matchOption 匹配用户回复文本与候选选项
func matchOption(input string, options []InquiryOption) (InquiryOption, bool) {
	clean := strings.TrimSpace(input)
	if clean == "" {
		return InquiryOption{}, false
	}

	// 1. 尝试纯数字序号 (例如 "1", "2", "【1】", "[1]")
	numStr := strings.Trim(clean, "[]【】()（）#号 ")
	if idx, err := strconv.Atoi(numStr); err == nil {
		if idx >= 1 && idx <= len(options) {
			return options[idx-1], true
		}
	}

	// 2. 精确或忽略大小写匹配
	lowerClean := strings.ToLower(clean)
	for _, opt := range options {
		if strings.EqualFold(opt.OptionID, lowerClean) || strings.EqualFold(opt.Name, clean) {
			return opt, true
		}
	}

	// 3. 子串模糊匹配 (如 "选择允许", "拒绝执行")
	for _, opt := range options {
		if opt.Name != "" && (strings.Contains(clean, opt.Name) || strings.Contains(opt.Name, clean)) {
			return opt, true
		}
		if opt.OptionID != "" && strings.Contains(lowerClean, strings.ToLower(opt.OptionID)) {
			return opt, true
		}
	}

	return InquiryOption{}, false
}
