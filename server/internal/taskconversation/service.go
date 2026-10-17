// Package taskconversation 提供飞书任务卡片、历史存储和续聊/取消动作。
package taskconversation

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/idgen"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// JobCanceller 负责更新状态并向工作站发送 STOP_JOB。
type JobCanceller interface {
	CancelJob(ctx context.Context, jobID, actorID, ip string) (*job.Job, error)
}

// Service 协调任务消息历史、Job 状态和同一张飞书共享卡片。
type Service struct {
	Store    Store
	Feishu   *feishu.Service
	Jobs     *job.Service
	Canceler JobCanceller
	FollowUp feishu.TaskFollowUpCreator

	mu    sync.Mutex
	dirty map[string]time.Time
}

func NewService(store Store, fs *feishu.Service, jobs *job.Service, canceler JobCanceller) *Service {
	return &Service{Store: store, Feishu: fs, Jobs: jobs, Canceler: canceler, dirty: make(map[string]time.Time)}
}

func (s *Service) SetFollowUpCreator(creator feishu.TaskFollowUpCreator) { s.FollowUp = creator }

// Start 创建一张用户任务会话卡片并落下首轮用户消息。
func (s *Service) Start(ctx context.Context, in feishu.TaskConversationStart) (string, error) {
	if s.Store == nil || in.JobID == "" || in.EmployeeID == "" {
		return "", fmt.Errorf("任务会话参数不完整")
	}
	// 飞书事件可能重投；同一个 Job 只能对应一张任务会话卡片。
	if existing, err := s.Store.FindByJob(ctx, in.JobID); err == nil {
		s.markDirty(existing.ID, 0)
		return existing.ID, nil
	}
	id := idgen.New("TC")
	now := time.Now().UTC()
	c := &Conversation{
		ID: id, EmployeeID: in.EmployeeID, EmployeeName: in.EmployeeName,
		OwnerUserID: in.OwnerUserID, OwnerOpenID: in.OwnerOpenID,
		ChatID: in.ChatID, CurrentJobID: in.JobID, Status: job.StatusCreated,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Store.Create(ctx, c); err != nil {
		return "", err
	}
	if err := s.Store.AttachTurn(ctx, id, in.JobID, in.Prompt); err != nil {
		return "", err
	}
	s.markDirty(id, 0)
	return id, nil
}

// AttachTurn 把后续 Job 的输入追加到同一任务会话。
func (s *Service) AttachTurn(ctx context.Context, conversationID, jobID, prompt string) error {
	if err := s.Store.AttachTurn(ctx, conversationID, jobID, prompt); err != nil {
		return err
	}
	s.markDirty(conversationID, 0)
	return nil
}

func (s *Service) ManagesJob(ctx context.Context, jobID string) bool {
	if s.Store == nil || jobID == "" {
		return false
	}
	_, err := s.Store.FindByJob(ctx, jobID)
	return err == nil
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
}

// Run 合并卡片更新，并在服务重启后刷新最近的任务卡片。
func (s *Service) Run(ctx context.Context) {
	ids, err := s.Store.ListIDs(ctx, 100)
	if err == nil {
		for _, id := range ids {
			s.markDirty(id, 0)
		}
	}
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.flushDue(ctx)
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
	for id, at := range s.dirty {
		if !now.Before(at) {
			due = append(due, id)
			delete(s.dirty, id)
		}
	}
	s.mu.Unlock()
	for _, id := range due {
		if err := s.refreshCard(ctx, id); err != nil {
			fmt.Printf("[TaskConversation] 刷新飞书卡片失败 (conversation=%s): %v\n", id, err)
			s.markDirty(id, 3*time.Second)
		}
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
	messages, err := s.Store.Messages(ctx, id)
	if err != nil {
		return err
	}
	activities, err := s.Store.Activities(ctx, id, c.CurrentJobID, 4)
	if err != nil {
		return err
	}
	content := BuildCard(c, messages, activities).MustJSON()
	if c.CardMessageID == "" {
		result, err := s.Feishu.SendMessage(ctx, "", c.ChatID, content)
		if err != nil {
			return err
		}
		if result == nil || result.MessageID == "" {
			return fmt.Errorf("飞书未返回卡片 message_id")
		}
		return s.Store.SetCardMessageID(ctx, id, result.MessageID)
	}
	return s.Feishu.PatchMessageCard(ctx, c.CardMessageID, content)
}

// HandleCardAction 处理停止按钮和表单续聊。
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
	if name != "task_stop" && name != "task_continue" {
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
	current, err := s.Jobs.Get(ctx, c.CurrentJobID)
	if err != nil {
		return taskToast("warning", "找不到当前任务"), nil
	}
	switch name {
	case "task_stop":
		if isTerminal(current.Status) {
			return taskToast("warning", "当前任务已经结束"), nil
		}
		if s.Canceler == nil {
			return taskToast("warning", "任务停止服务暂不可用"), nil
		}
		_, err := s.Canceler.CancelJob(ctx, current.ID, c.OwnerUserID, "feishu-card")
		if err != nil {
			return taskToast("warning", "停止任务失败："+err.Error()), nil
		}
		s.OnJobStatus(ctx, current.ID, job.StatusCancelled, current.Result)
		return taskToast("success", "已发送停止指令"), nil
	case "task_continue":
		if !isTerminal(current.Status) {
			return taskToast("warning", "当前轮次仍在执行，请等待完成后继续"), nil
		}
		prompt, _ := action.FormValue["prompt"].(string)
		prompt = strings.TrimSpace(prompt)
		if prompt == "" {
			return taskToast("warning", "请输入要继续的内容"), nil
		}
		if len([]rune(prompt)) > 1000 {
			return taskToast("warning", "续聊内容不能超过 1000 个字符"), nil
		}
		if s.FollowUp == nil {
			return taskToast("warning", "续聊服务暂不可用"), nil
		}
		eventID := ""
		if event.EventV2Base != nil && event.EventV2Base.Header != nil {
			eventID = event.EventV2Base.Header.EventID
		}
		if eventID == "" {
			eventID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		conversationContext := ""
		if current.Status == job.StatusCancelled {
			if messages, messageErr := s.Store.Messages(ctx, c.ID); messageErr == nil {
				conversationContext = formatContext(messages)
			}
		}
		_, err := s.FollowUp.CreateFollowUp(ctx, feishu.TaskConversationFollowUp{
			ConversationID: c.ID, ParentJobID: current.ID, EmployeeID: c.EmployeeID,
			OwnerUserID: c.OwnerUserID, OwnerOpenID: c.OwnerOpenID, ChatID: c.ChatID,
			Prompt: prompt, Context: conversationContext,
			IdempotencyKey: "feishu-task:" + c.ID + ":" + eventID,
		})
		if err != nil {
			return taskToast("warning", "创建续聊任务失败："+err.Error()), nil
		}
		return taskToast("success", "已提交续聊任务"), nil
	}
	return nil, nil
}

func formatContext(messages []Message) string {
	if len(messages) > 12 {
		messages = messages[len(messages)-12:]
	}
	var b strings.Builder
	b.WriteString("【此前任务对话】\n")
	for _, m := range messages {
		name := "用户"
		if m.Role == "assistant" {
			name = "AI 员工"
		}
		b.WriteString(name + "：" + truncateRunes(m.Content, 1200) + "\n")
	}
	return truncateRunes(b.String(), 8000)
}

func taskToast(kind, content string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: content}}
}
