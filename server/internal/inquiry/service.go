// Package inquiry 提供工作站 AI Agent 询问/决策向飞书通知与交互全链路处理。
package inquiry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ai-employee-platform/server/internal/statepg"
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
	EmployeeName         string          `json:"employee_name,omitempty"`
	CardMessageID        string          `json:"card_message_id,omitempty"`
	SubmittedCardUpdated bool            `json:"submitted_card_updated,omitempty"`
	AllowText            bool            `json:"allow_text,omitempty"`
	ID                   string          `json:"id"`
	JobID                string          `json:"job_id"`
	WorkstationID        string          `json:"workstation_id"`
	EmployeeID           string          `json:"employee_id"`
	ApproverOpenID       string          `json:"approver_open_id"`
	ChatID               string          `json:"chat_id"`
	Message              string          `json:"message"`
	Options              []InquiryOption `json:"options"`
	Status               string          `json:"status"` // PENDING | RESOLVED | EXPIRED
	SelectedOptionID     string          `json:"selected_option_id,omitempty"`
	SelectedOptionName   string          `json:"selected_option_name,omitempty"`
	ResolvedBy           string          `json:"resolved_by,omitempty"`
	ResolvedVia          string          `json:"resolved_via,omitempty"`
	CreatedAt            time.Time       `json:"created_at"`
	ResolvedAt           time.Time       `json:"resolved_at,omitempty"`
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

	Persistent     *statepg.Collection
	commands       CommandPusher
	jobs           JobManager
	employees      EmployeeGetter
	feishu         *feishu.Service
	chatResolver   ChatResolver
	ApproverForJob func(context.Context, string) string
	OnJobStatus    func(context.Context, string, string, string)
	UpdateCard     func(context.Context, string, string) error
}

// NewService 创建 Inquiry 服务
func NewService(cmd CommandPusher, jm JobManager, eg EmployeeGetter, fs *feishu.Service, cr ChatResolver) *Service {
	s := &Service{
		inquiries:    make(map[string]*Inquiry),
		byChat:       make(map[string]*Inquiry),
		byJob:        make(map[string]*Inquiry),
		commands:     cmd,
		jobs:         jm,
		employees:    eg,
		feishu:       fs,
		chatResolver: cr,
	}
	if fs != nil {
		s.UpdateCard = fs.PatchMessageCard
	}
	return s
}

func (s *Service) transition(ctx context.Context, id, status, actor, source string, meta map[string]string) error {
	_, err := s.jobs.Transition(ctx, id, status, actor, source, meta)
	if err == nil && s.OnJobStatus != nil {
		s.OnJobStatus(ctx, id, status, "")
	}
	return err
}

// HandleInquiryEvent 处理工作站上报的 EVENT_TYPE_JOB_INQUIRY 事件
func (s *Service) HandleInquiryEvent(ctx context.Context, wsID string, ev *aiev1.Event) {
	if ev == nil {
		return
	}
	var payload struct {
		AllowText  bool            `json:"allow_text"`
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
	if len(options) == 0 && !payload.AllowText {
		options = []InquiryOption{
			{OptionID: "allow-once", Name: "允许本次执行"},
			{OptionID: "deny", Name: "拒绝执行"},
		}
	}

	if s.jobs == nil || s.ApproverForJob == nil {
		return
	}
	j, err := s.jobs.Get(ctx, jobID)
	if err != nil || j == nil || j.WorkstationID != wsID || (empID != "" && empID != j.EmployeeID) {
		return
	}
	empID = j.EmployeeID
	approver := s.ApproverForJob(ctx, jobID)
	if approver == "" {
		return
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
		EmployeeName:   empName,
		ID:             inquiryID,
		AllowText:      payload.AllowText,
		ApproverOpenID: approver,
		JobID:          jobID,
		WorkstationID:  wsID,
		EmployeeID:     empID,
		ChatID:         chatID,
		Message:        msgText,
		Options:        options,
		Status:         "PENDING",
		CreatedAt:      time.Now().UTC(),
	}

	s.mu.Lock()
	if previous := s.inquiries[inquiryID]; previous != nil {
		s.mu.Unlock()
		return
	}
	for id, old := range s.inquiries {
		if time.Since(old.CreatedAt) > 30*time.Minute {
			delete(s.inquiries, id)
			if s.byJob[old.JobID] == old {
				delete(s.byJob, old.JobID)
			}
			if s.byChat[old.ChatID] == old {
				delete(s.byChat, old.ChatID)
			}
		}
	}
	if old := s.byJob[jobID]; old != nil {
		old.Status = "EXPIRED"
		if s.Persistent != nil {
			if err := s.Persistent.Put(ctx, old.ID, old); err != nil {
				s.mu.Unlock()
				return
			}
		}
	}
	if s.Persistent != nil {
		if err := s.Persistent.Insert(ctx, inquiryID, inq); err != nil {
			s.mu.Unlock()
			return
		}
	}
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
		if err := s.transition(ctx, jobID, job.StatusWaitingApproval, "workstation_inquiry", wsID, map[string]string{
			"inquiry_id": inquiryID,
		}); err != nil {
			return
		}
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
		card := feishu.BuildInquiryCard(jobID, empName, inquiryID, msgText, cardOpts, inq.AllowText)
		send := func(ctx context.Context) error {
			_, sdkSender := s.feishu.Sender.(*feishu.SDKSender)
			if s.feishu.Sender != nil && !sdkSender {
				return s.feishu.Sender.Send(ctx, feishu.Reply{ChatID: chatID, Content: card.MustJSON()})
			}
			result, err := s.feishu.SendMessageWithKey(ctx, "", chatID, card.MustJSON(), inquiryID)
			if err != nil {
				return err
			}
			if result != nil && result.MessageID != "" {
				return s.rememberCard(ctx, inquiryID, result.MessageID)
			}
			return fmt.Errorf("飞书未返回确认卡片 message_id")
		}
		deliveryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		err := send(deliveryCtx)
		cancel()
		if err != nil {
			_ = s.jobs.AppendEvent(ctx, jobID, "INQUIRY_DELIVERY_FAILED", map[string]string{"inquiry_id": inquiryID, "message": "飞书确认卡发送失败，正在重试"})
			go s.retryCard(ctx, inquiryID, send)
			fmt.Printf("[Inquiry] ⚠️ 发送飞书选项卡片失败 (chat=%s): %v\n", chatID, err)
		} else {
			fmt.Printf("[Inquiry] 🔔 成功向飞书发送决策请示卡片: inq=%s, job=%s, chat=%s\n", inquiryID, jobID, chatID)
		}
	}
}

// A duplicate delivery carries the same inquiry ID and can only be answered once.
func (s *Service) retryCard(ctx context.Context, id string, send func(context.Context) error) {
	for delay := 3 * time.Second; ; delay = min(delay*2, 30*time.Second) {
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		inq := s.GetInquiry(id)
		if inq == nil || inq.Status != "PENDING" || time.Since(inq.CreatedAt) >= 10*time.Minute {
			return
		}
		j, err := s.jobs.Get(ctx, inq.JobID)
		if err != nil {
			continue
		}
		if j == nil || j.Status != job.StatusWaitingApproval {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		err = send(attempt)
		cancel()
		if err == nil {
			return
		}
	}
}

// HandleCardAction 处理飞书卡片按钮点击回调
func (s *Service) HandleCardAction(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return &callback.CardActionTriggerResponse{}, nil
	}

	action := event.Event.Action
	val := action.Value
	act, _ := val["action"].(string)
	inquiryID, _ := val["inquiry_id"].(string)
	optionID, _ := val["option_id"].(string)
	optionName, _ := val["option_name"].(string)
	via := "飞书卡片按钮点击"
	if strings.HasPrefix(action.Name, feishu.InquiryTextSubmitPrefix) && action.Tag == "button" {
		inquiryID = strings.TrimPrefix(action.Name, feishu.InquiryTextSubmitPrefix)
		answer, ok := action.FormValue[feishu.InquiryAnswerField].(string)
		if !ok || strings.TrimSpace(answer) == "" || len([]rune(answer)) > 1000 {
			return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: "warning", Content: "请输入 1 至 1000 字的回答后提交"}}, nil
		}
		optionID = "text:" + answer
		via = "飞书卡片输入框"
	} else if act != "resolve_inquiry" {
		return &callback.CardActionTriggerResponse{}, nil
	}

	openID := ""
	if event.Event.Operator != nil {
		openID = event.Event.Operator.OpenID
	}
	// Older persisted inquiries may not have captured their original message ID.
	if event.Event.Context != nil {
		if pending := s.GetInquiry(inquiryID); pending != nil && pending.ApproverOpenID == openID && pending.CardMessageID == "" && event.Event.Context.OpenMessageID != "" {
			if err := s.rememberCard(ctx, inquiryID, event.Event.Context.OpenMessageID); err != nil {
				return nil, err
			}
		}
	}

	inq, err := s.ResolveInquiry(ctx, inquiryID, optionID, optionName, openID, via)
	if err != nil {
		if inq != nil && inq.Status == "RESOLVED" {
			s.updateSubmittedCard(*inq)
		}
		return &callback.CardActionTriggerResponse{
			Toast: &callback.Toast{
				Type:    "warning",
				Content: fmt.Sprintf("处理失败：%v", err),
			},
		}, nil
	}

	// 告知飞书客户端成功提示
	answerPreview := []rune(inq.SelectedOptionName)
	if len(answerPreview) > 80 {
		answerPreview = answerPreview[:80]
	}
	toastText := "已提交：" + string(answerPreview)
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

	parts := strings.Fields(text)
	if len(parts) < 2 {
		return false, nil
	}
	s.mu.RLock()
	original := s.inquiries[parts[0]]
	var inq *Inquiry
	if original != nil {
		snapshot := *original
		inq = &snapshot
	}
	s.mu.RUnlock()
	if inq == nil || (inq.ChatID != chatID && inq.ChatID != senderOpenID) {
		return false, nil
	}
	text = strings.Join(parts[1:], " ")

	if inq == nil || inq.Status != "PENDING" {
		return false, nil
	}

	// 匹配选项
	matchedOpt, ok := matchOption(text, inq.Options)
	if !ok && inq.AllowText && len([]rune(text)) <= 2000 {
		matchedOpt = InquiryOption{OptionID: "text:" + text, Name: text}
		ok = true
	}
	if !ok {
		return false, nil
	}

	_, err := s.ResolveInquiry(ctx, inq.ID, matchedOpt.OptionID, matchedOpt.Name, senderOpenID, "飞书会话直接回复")
	if err != nil {
		return true, err
	}

	// 在会话中回复确认反馈
	if s.feishu != nil && chatID != "" {
		replyText := fmt.Sprintf("✅ 收到回复确认：已选择【%s】，已提交选择，工作站将继续执行任务。", matchedOpt.Name)
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
		return inq, fmt.Errorf("请示 %s 已于 %s 完成处理 (状态=%s)", inquiryID, feishu.FormatCardTime(inq.ResolvedAt), inq.Status)
	}

	if s.byJob[inq.JobID] != inq {
		s.mu.Unlock()
		return nil, fmt.Errorf("此请求已被新的请示替代")
	}
	if actor == "" || actor != inq.ApproverOpenID {
		s.mu.Unlock()
		return nil, fmt.Errorf("只有任务发起者可以确认此请求")
	}
	if time.Since(inq.CreatedAt) > 10*time.Minute {
		inq.Status = "EXPIRED"
		s.mu.Unlock()
		return nil, fmt.Errorf("请求已过期")
	}
	if s.jobs == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("任务校验不可用")
	}
	j, err := s.jobs.Get(ctx, inq.JobID)
	if err != nil || j == nil || j.WorkstationID != inq.WorkstationID || j.EmployeeID != inq.EmployeeID || j.Status != job.StatusWaitingApproval {
		s.mu.Unlock()
		return nil, fmt.Errorf("任务已结束或归属不符")
	}
	validOption := false
	for _, o := range inq.Options {
		if o.OptionID == optionID {
			optionName = o.Name
			validOption = true
			break
		}
	}
	if inq.AllowText && strings.HasPrefix(optionID, "text:") {
		optionName = strings.TrimSpace(strings.TrimPrefix(optionID, "text:"))
		validOption = optionName != "" && len([]rune(optionName)) <= 2000
	}
	if !validOption {
		s.mu.Unlock()
		return nil, fmt.Errorf("无效选项")
	}
	if s.commands == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("工作站命令队列不可用")
	}
	next := *inq
	next.Status = "RESOLVED"
	next.SelectedOptionID, next.SelectedOptionName = optionID, optionName
	next.ResolvedBy, next.ResolvedVia, next.ResolvedAt = actor, via, time.Now().UTC()
	if s.Persistent != nil {
		claimed, err := s.Persistent.ResolvePending(ctx, inq.ID, &next)
		if err != nil || !claimed {
			s.mu.Unlock()
			return nil, fmt.Errorf("请示已处理或持久化不可用")
		}
	}
	meta := map[string]string{"inquiry_id": inq.ID, "option_id": optionID, "option_name": optionName, "by": actor, "via": via}
	// A quick answer can finish the job as soon as the worker receives the command.
	if err = s.transition(ctx, inq.JobID, job.StatusRunning, "inquiry_resolved", actor, meta); err == nil {
		payload, _ := json.Marshal(map[string]string{"inquiry_id": inq.ID, "selected_option_id": optionID, "selected_option_name": optionName, "resolved_by": actor, "resolved_via": via})
		cmd, pushErr := s.commands.PushCommand(inq.WorkstationID, aiev1.CommandType_COMMAND_TYPE_RESOLVE_INQUIRY, inq.EmployeeID, inq.JobID, string(payload))
		// A non-nil command has already been queued, including for reconnect.
		if pushErr != nil && cmd == nil {
			rollbackErr := s.transition(ctx, inq.JobID, job.StatusWaitingApproval, "inquiry_retry", actor, meta)
			err = errors.Join(pushErr, rollbackErr)
		}
	}
	if err != nil {
		if s.Persistent != nil {
			err = errors.Join(err, s.Persistent.Put(ctx, inq.ID, inq))
		}
		s.mu.Unlock()
		return nil, fmt.Errorf("未能下发选择：%w", err)
	}
	*inq = next
	if s.byChat[inq.ChatID] == inq {
		delete(s.byChat, inq.ChatID)
	}
	delete(s.byJob, inq.JobID)
	resolved := *inq
	s.mu.Unlock()
	_ = s.jobs.AppendEvent(ctx, inq.JobID, "INQUIRY_RESOLVED", meta)
	s.updateSubmittedCard(resolved)

	return &resolved, nil
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

	// Accept explicit choice wording, never interpret negation as approval.
	for _, prefix := range []string{"我选择", "我选", "选择"} {
		if value, ok := strings.CutPrefix(clean, prefix); ok {
			for _, opt := range options {
				if strings.TrimSpace(value) == opt.Name {
					return opt, true
				}
			}
		}
	}
	return InquiryOption{}, false
}

func (s *Service) SetPersistentStore(ctx context.Context, data *statepg.Collection) error {
	all, err := data.All(ctx)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, b := range all {
		var inq Inquiry
		if err = json.Unmarshal(b, &inq); err != nil {
			return err
		}
		if inq.Status == "RESOLVED" {
			s.inquiries[inq.ID] = &inq
			if !inq.SubmittedCardUpdated {
				s.updateSubmittedCard(inq)
			}
			continue
		}
		if inq.Status != "PENDING" || time.Since(inq.CreatedAt) > 10*time.Minute {
			continue
		}
		if current := s.byJob[inq.JobID]; current != nil {
			if current.CreatedAt.After(inq.CreatedAt) {
				continue
			}
			current.Status = "EXPIRED"
		}
		s.inquiries[inq.ID] = &inq
		if current := s.byChat[inq.ChatID]; current == nil || inq.CreatedAt.After(current.CreatedAt) {
			s.byChat[inq.ChatID] = &inq
		}
		s.byJob[inq.JobID] = &inq
	}
	s.Persistent = data
	return nil
}
