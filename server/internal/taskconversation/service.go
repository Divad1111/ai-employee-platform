// Package taskconversation 提供飞书任务卡片、历史存储和续聊/取消动作。
package taskconversation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/idgen"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/workstation"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// JobCanceller 负责更新状态并向工作站发送 STOP_JOB。
type JobCanceller interface {
	CancelJob(ctx context.Context, jobID, actorID, ip string) (*job.Job, error)
}

// Service 协调每个 Job 独立的飞书卡片，以及员工级 ACP 会话上下文。
type Service struct {
	Store        Store
	Feishu       *feishu.Service
	Jobs         *job.Service
	Canceler     JobCanceller
	Employees    *employee.Service
	Workstations *workstation.Service

	mu             sync.Mutex
	dirty          map[string]time.Time
	active         map[string]bool
	inFlight       map[string]bool
	refreshSlots   chan struct{}
	refreshTimeout time.Duration
}

func NewService(store Store, fs *feishu.Service, jobs *job.Service, canceler JobCanceller) *Service {
	return &Service{Store: store, Feishu: fs, Jobs: jobs, Canceler: canceler, dirty: make(map[string]time.Time), active: make(map[string]bool), inFlight: make(map[string]bool), refreshSlots: make(chan struct{}, 4), refreshTimeout: 12 * time.Second}
}

// Start 为每个 Job 建立独立飞书卡片；员工的 ACP 会话由 session generation 复用。
func (s *Service) Start(ctx context.Context, in feishu.TaskConversationStart) (string, error) {
	if s.Store == nil || in.JobID == "" || in.EmployeeID == "" {
		return "", fmt.Errorf("任务会话参数不完整")
	}
	// 飞书事件可能重投；同一个 Job 只能对应一张卡片。
	if existing, err := s.Store.FindByJob(ctx, in.JobID); err == nil {
		s.markDirty(existing.ID, 0)
		return existing.ID, nil
	}
	now := time.Now().UTC()
	c := &Conversation{
		ID: idgen.New("TC"), EmployeeID: in.EmployeeID, EmployeeName: in.EmployeeName,
		OwnerUserID: in.OwnerUserID, OwnerOpenID: in.OwnerOpenID,
		ChatID: in.ChatID, CurrentJobID: in.JobID, Status: job.StatusCreated,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Store.CreateTask(ctx, c, in.Prompt, in.SessionGeneration); err != nil {
		if existing, findErr := s.Store.FindByJob(ctx, in.JobID); findErr == nil {
			s.markDirty(existing.ID, 0)
			return existing.ID, nil
		}
		return "", err
	}
	s.markDirty(c.ID, 0)
	s.trackActive(c.ID, true)
	return c.ID, nil
}

func (s *Service) ManagesJob(ctx context.Context, jobID string) bool {
	if s.Store == nil || jobID == "" {
		return false
	}
	_, err := s.Store.FindByJob(ctx, jobID)
	return err == nil
}

// CanScheduleJob serializes queued turns so one ACP session is never used by
// two jobs at once. Unmanaged jobs pass through unchanged.
func (s *Service) CanScheduleJob(ctx context.Context, jobID string) (bool, error) {
	c, err := s.Store.FindByJob(ctx, jobID)
	if errors.Is(err, ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	ids, err := s.Store.SessionJobIDs(ctx, c.EmployeeID)
	if err != nil {
		return false, err
	}
	for _, id := range ids {
		turn, err := s.Jobs.Get(ctx, id)
		if err != nil {
			continue
		}
		if isTerminal(turn.Status) {
			continue
		}
		return id == jobID, nil
	}
	return false, nil
}

// ConversationContext returns recent messages from the same explicit session
// generation, for restoring ACP context after a workstation process restart.
func (s *Service) ConversationContext(ctx context.Context, jobID string) (string, error) {
	c, err := s.Store.FindByJob(ctx, jobID)
	if err != nil {
		return "", err
	}
	messages, err := s.Store.Messages(ctx, c.ID)
	if err != nil {
		return "", err
	}
	var generation int64
	found := false
	for _, message := range messages {
		if message.JobID == jobID {
			generation, found = message.SessionGeneration, true
			break
		}
	}
	if !found {
		return "", nil
	}
	messages, err = s.Store.SessionMessages(ctx, c.EmployeeID, generation)
	if err != nil {
		return "", err
	}
	var prior []Message
	for _, message := range messages {
		if message.JobID != jobID && message.SessionGeneration == generation && message.Content != "" {
			prior = append(prior, message)
		}
	}
	if len(prior) > 20 {
		prior = prior[len(prior)-20:]
	}
	var b strings.Builder
	b.WriteString("【此前同一会话的消息，仅用于理解背景；不要重复执行历史任务】\n")
	for _, message := range prior {
		label := "用户"
		if message.Role == "assistant" {
			label = "AI 员工"
		}
		b.WriteString(label + "：" + truncateRunes(message.Content, 1600) + "\n")
	}
	return truncateRunes(b.String(), 12000), nil
}

// HandleEmployeeCommand consumes employee-scoped Feishu controls. The user
// must address an employee explicitly, just like when submitting a task.
func (s *Service) HandleEmployeeCommand(ctx context.Context, employeeID, command, senderOpenID string) (string, bool) {
	kind, args, ok := parseEmployeeCommand(command)
	if !ok {
		return "", false
	}
	e, err := s.Employees.Get(ctx, employeeID)
	if err != nil || e == nil {
		return "找不到该 AI 员工。", true
	}
	if s.Feishu == nil || !s.Feishu.EmployeeHasOpenID(employeeID, senderOpenID) {
		return "当前飞书账号未绑定该 AI 员工，不能修改会话或模型。", true
	}
	switch kind {
	case "new_session":
		updated, err := s.Employees.StartNewSession(ctx, employeeID, e.OwnerUserID, senderOpenID)
		if err != nil {
			return "创建新会话失败：" + err.Error(), true
		}
		s.RefreshEmployee(ctx, employeeID)
		return fmt.Sprintf("已为 %s 创建新的会话代数（%d）。从下一条任务开始使用新会话。", e.Name, updated.SessionGeneration), true
	case "switch_model":
		return s.switchEmployeeModel(ctx, e, args, senderOpenID), true
	default:
		return "不支持的员工命令。", true
	}
}

func (s *Service) RefreshEmployee(ctx context.Context, employeeID string) {
	if ids, err := s.Store.ListEmployeeIDs(ctx, employeeID, 100); err == nil {
		for _, id := range ids {
			s.markDirty(id, 0)
		}
	}
}

func (s *Service) switchEmployeeModel(ctx context.Context, e *employee.Employee, args, actor string) string {
	if s.Workstations == nil || e.WorkstationID == "" {
		return "无法读取工作站的 Provider 和模型列表。"
	}
	parts := strings.Fields(args)
	if len(parts) == 0 {
		return "用法：切换模型 <模型>，或切换模型 <Provider> <模型>。"
	}
	provider := e.DefaultProvider
	model := strings.Join(parts, " ")
	if len(parts) > 1 {
		candidate := strings.ToLower(parts[0])
		view := s.Workstations.Get(ctx, e.WorkstationID)
		for _, available := range view.Providers {
			if strings.EqualFold(candidate, available) {
				provider = available
				model = strings.Join(parts[1:], " ")
				break
			}
		}
		if provider == e.DefaultProvider && len(view.Providers) == 0 {
			for available := range view.Models {
				if strings.EqualFold(candidate, available) {
					provider = available
					model = strings.Join(parts[1:], " ")
					break
				}
			}
		}
	}
	if provider == "" {
		return "请指定 Provider，例如：切换模型 codex gpt-5.6-terra。"
	}
	view := s.Workstations.Get(ctx, e.WorkstationID)
	providerFound := false
	for _, available := range view.Providers {
		if strings.EqualFold(provider, available) {
			provider, providerFound = available, true
			break
		}
	}
	if !providerFound {
		for available := range view.Models {
			if strings.EqualFold(provider, available) {
				provider, providerFound = available, true
				break
			}
		}
	}
	if !providerFound {
		return "工作站未安装 Provider：" + provider
	}
	options := view.Models[provider]
	if len(options) == 0 {
		return fmt.Sprintf("工作站尚未上报 Provider %s 的模型列表，请先确认工作站在线并完成模型同步。", provider)
	}
	modelFound := false
	for _, option := range options {
		if strings.EqualFold(model, option.ID) || (option.Label != "" && strings.EqualFold(model, option.Label)) {
			model, modelFound = option.ID, true
			break
		}
	}
	if !modelFound {
		return fmt.Sprintf("Provider %s 没有可用模型 %q；请使用工作站上报的模型 ID 或名称。", provider, model)
	}
	updated, err := s.Employees.Update(ctx, e.ID, employee.UpdateInput{DefaultProvider: &provider, DefaultModel: &model}, e.OwnerUserID, actor)
	if err != nil {
		return "切换模型失败：" + err.Error()
	}
	s.RefreshEmployee(ctx, e.ID)
	return fmt.Sprintf("已切换到 Provider %s，模型 %s。后续任务会使用新配置。", updated.DefaultProvider, updated.DefaultModel)
}

func parseEmployeeCommand(input string) (kind, args string, ok bool) {
	text := strings.TrimSpace(input)
	lower := strings.ToLower(text)
	for _, prefix := range []string{"新建一个会话", "新建会话", "新起一个会话", "新开会话", "新会话", "/new-session", "/new_session"} {
		if lower == strings.ToLower(prefix) || strings.HasPrefix(lower, strings.ToLower(prefix)+" ") {
			return "new_session", "", true
		}
	}
	for _, prefix := range []string{"切换模型", "切换到模型", "更换模型", "/model"} {
		p := strings.ToLower(prefix)
		if prefix == "/model" && lower != p && !strings.HasPrefix(lower, p+" ") {
			continue
		}
		if strings.HasPrefix(lower, p) {
			args = strings.TrimSpace(text[len(prefix):])
			for _, leading := range []string{"到", "为", "使用"} {
				args = strings.TrimSpace(strings.TrimPrefix(args, leading))
			}
			return "switch_model", args, true
		}
	}
	return "", "", false
}

// OnAgentDelta 持久化一个有序、幂等的面向用户的 Agent 回复片段。
func (s *Service) OnAgentDelta(ctx context.Context, eventID, jobID string, sequence uint64, text string) {
	if text == "" || s.Store == nil {
		return
	}
	if eventID == "" {
		eventID = fmt.Sprintf("%s:%d", jobID, sequence)
	}
	c, err := s.Store.FindByJob(ctx, jobID)
	if err != nil {
		return
	}
	if err := s.Store.AppendDelta(ctx, eventID, jobID, sequence, text); err != nil {
		fmt.Printf("[TaskConversation] 保存 Agent 增量失败 (job=%s): %v\n", jobID, err)
		return
	}
	s.markDirty(c.ID, 0)
}

// OnAgentActivity 保存并展示一条简短的用户可见进度事件。
func (s *Service) OnAgentActivity(ctx context.Context, eventID, jobID string, sequence uint64, text string) {
	text = strings.TrimSpace(text)
	if text == "" || s.Store == nil {
		return
	}
	if eventID == "" {
		eventID = fmt.Sprintf("%s:activity:%d", jobID, sequence)
	}
	c, err := s.Store.FindByJob(ctx, jobID)
	if err != nil {
		return
	}
	if err := s.Store.AppendActivity(ctx, eventID, jobID, sequence, text); err != nil {
		fmt.Printf("[TaskConversation] 保存 Agent 进度失败 (job=%s): %v\n", jobID, err)
		return
	}
	s.markDirty(c.ID, 0)
}

// OnJobStatus 把 Job 状态及最终回复写入任务会话历史。
func (s *Service) OnJobStatus(ctx context.Context, jobID, status, reply string) {
	if s.Store == nil {
		return
	}
	c, err := s.Store.FindByJob(ctx, jobID)
	if err != nil {
		return
	}
	if err := s.Store.SetTurnStatus(ctx, jobID, status); err != nil {
		fmt.Printf("[TaskConversation] 更新轮次状态失败 (job=%s): %v\n", jobID, err)
	}
	if reply != "" {
		if err := s.Store.SetAssistantFinal(ctx, jobID, reply, status); err != nil {
			fmt.Printf("[TaskConversation] 保存 Agent 完整回复失败 (job=%s): %v\n", jobID, err)
		}
	}
	s.markDirty(c.ID, 0)
	s.trackActive(c.ID, !isTerminal(status))
}

func (s *Service) OnAgentSession(ctx context.Context, jobID, provider, model string, generation int64) {
	if s.Store == nil {
		return
	}
	c, err := s.Store.FindByJob(ctx, jobID)
	if err != nil {
		return
	}
	if err := s.Store.SetRuntime(ctx, jobID, provider, model, generation); err != nil {
		fmt.Printf("[TaskConversation] 更新 Provider/模型失败 (job=%s): %v\n", jobID, err)
		return
	}
	s.markDirty(c.ID, 0)
}

// Run restores cards after restart and refreshes active cards even while the
// model has no new text. Each network request has its own timeout and slot.
func (s *Service) Run(ctx context.Context) {
	ids, err := s.Store.ListIDs(ctx, 0)
	if err == nil {
		for _, id := range ids {
			if c, err := s.Store.Get(ctx, id); err == nil {
				s.trackActive(id, !isTerminal(c.Status))
				s.markDirty(id, 0)
			}
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	heartbeat := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.flushDue(ctx)
		case <-heartbeat.C:
			s.markActiveDirty()
		}
	}
}

func (s *Service) trackActive(id string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if active {
		s.active[id] = true
	} else {
		delete(s.active, id)
	}
}

func (s *Service) markActiveDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.active {
		if _, exists := s.dirty[id]; !exists {
			s.dirty[id] = time.Now()
		}
	}
}

func (s *Service) markDirty(id string, delay time.Duration) {
	if id == "" {
		return
	}
	s.mu.Lock()
	due := time.Now().Add(delay)
	if prev, ok := s.dirty[id]; !ok || due.Before(prev) {
		s.dirty[id] = due
	}
	s.mu.Unlock()
}

func (s *Service) flushDue(ctx context.Context) {
	now := time.Now()
	s.mu.Lock()
	var due []string
	var candidates []string
	for id := range s.dirty {
		candidates = append(candidates, id)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if s.active[candidates[i]] != s.active[candidates[j]] {
			return s.active[candidates[i]]
		}
		return s.dirty[candidates[i]].Before(s.dirty[candidates[j]])
	})
	for _, id := range candidates {
		at := s.dirty[id]
		if !now.Before(at) && !s.inFlight[id] {
			select {
			case s.refreshSlots <- struct{}{}:
			default:
				continue
			}
			s.inFlight[id] = true
			due = append(due, id)
			delete(s.dirty, id)
		}
	}
	s.mu.Unlock()
	for _, id := range due {
		go func(id string) {
			defer func() {
				s.mu.Lock()
				delete(s.inFlight, id)
				s.mu.Unlock()
				<-s.refreshSlots
			}()
			requestCtx, cancel := context.WithTimeout(ctx, s.refreshTimeout)
			defer cancel()
			if err := s.refreshCard(requestCtx, id); err != nil {
				fmt.Printf("[TaskConversation] 刷新飞书卡片失败 (conversation=%s): %v\n", id, err)
				s.markDirty(id, 3*time.Second)
			}
		}(id)
	}
}

func (s *Service) refreshCard(ctx context.Context, id string) error {
	c, err := s.Store.Get(ctx, id)
	if err != nil {
		return err
	}
	if c.ChatID == "" || s.Feishu == nil {
		return nil
	}
	binding := s.Feishu.BindingByEmployee(c.EmployeeID)
	if binding == nil || !binding.Verified || binding.OwnerUserID != c.OwnerUserID || s.Feishu.AuthorizeActor == nil || s.Feishu.AuthorizeActor(ctx, c.EmployeeID, c.OwnerOpenID) != nil {
		s.trackActive(id, false)
		return nil
	}
	s.trackActive(id, !isTerminal(c.Status))
	messages, err := s.Store.Messages(ctx, id)
	if err != nil {
		return err
	}
	activities, err := s.Store.Activities(ctx, id, c.CurrentJobID, 10)
	if err != nil {
		return err
	}
	if s.Employees != nil {
		if e, employeeErr := s.Employees.Get(ctx, c.EmployeeID); employeeErr == nil && e != nil {
			c.DefaultProvider = e.DefaultProvider
			c.DefaultModel = e.DefaultModel
		}
	}
	content := BuildCard(c, messages, activities).MustJSON()
	if c.CardMessageID == "" {
		result, err := s.Feishu.SendMessageWithKey(ctx, "", c.ChatID, content, c.ID)
		if err != nil {
			return err
		}
		if result == nil || result.MessageID == "" {
			return fmt.Errorf("飞书未返回卡片 message_id")
		}
		if err := s.Store.SetCardMessageID(ctx, id, result.MessageID); err != nil {
			return err
		}
		fmt.Printf("[TaskConversation] 飞书收单卡片已送达 (job=%s conversation=%s message=%s)\n", c.CurrentJobID, id, result.MessageID)
		if s.Jobs != nil {
			_ = s.Jobs.AppendEvent(ctx, c.CurrentJobID, "FEISHU_RECEIPT", map[string]string{"message": "飞书收单卡片已送达", "card_message_id": result.MessageID})
		}
		return nil
	}
	return s.Feishu.PatchMessageCard(ctx, c.CardMessageID, content)
}

// HandleCardAction 只处理正在执行任务的停止按钮。
func (s *Service) HandleCardAction(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	if event == nil || event.Event == nil || event.Event.Action == nil {
		return nil, nil
	}
	action := event.Event.Action
	value := action.Value
	if value == nil {
		return nil, nil
	}
	name, _ := value["action"].(string)
	if name != "task_stop" {
		return nil, nil
	}
	conversationID, _ := value["conversation_id"].(string)
	c, err := s.Store.Get(ctx, conversationID)
	if err != nil {
		return taskToast("warning", "找不到这条任务会话"), nil
	}
	operatorID := ""
	if event.Event.Operator != nil {
		operatorID = event.Event.Operator.OpenID
	}
	if operatorID == "" || operatorID != c.OwnerOpenID {
		return taskToast("warning", "只有发起任务的用户可以操作这张卡片"), nil
	}
	if s.Feishu == nil || s.Feishu.AuthorizeActor == nil || s.Feishu.AuthorizeActor(ctx, c.EmployeeID, operatorID) != nil {
		return taskToast("warning", "当前绑定未确认或账号无权操作"), nil
	}
	current, err := s.Jobs.Get(ctx, c.CurrentJobID)
	if err != nil {
		return taskToast("warning", "找不到当前任务"), nil
	}
	if isTerminal(current.Status) {
		return taskToast("warning", "当前任务已经结束"), nil
	}
	if s.Canceler == nil {
		return taskToast("warning", "任务停止服务暂不可用"), nil
	}
	_, err = s.Canceler.CancelJob(ctx, current.ID, c.OwnerUserID, "feishu-card")
	if err != nil {
		return taskToast("warning", "停止任务失败："+err.Error()), nil
	}
	s.OnJobStatus(ctx, current.ID, job.StatusCancelled, current.Result)
	return taskToast("success", "已发送停止指令"), nil
}

func taskToast(kind, content string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: content}}
}
