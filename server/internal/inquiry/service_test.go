package inquiry_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/inquiry"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type mockCommandPusher struct {
	mu       sync.Mutex
	commands []*aiev1.Command
}

func (m *mockCommandPusher) PushCommand(wsID string, typ aiev1.CommandType, employeeID, jobID, payloadJSON string) (*aiev1.Command, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd := &aiev1.Command{
		WorkstationId: wsID,
		Type:          typ,
		EmployeeId:    employeeID,
		JobId:         jobID,
		PayloadJson:   payloadJSON,
	}
	m.commands = append(m.commands, cmd)
	return cmd, nil
}

type mockJobManager struct {
	mu          sync.Mutex
	jobs        map[string]*job.Job
	transitions []string
	events      []string
}

func (m *mockJobManager) Get(_ context.Context, id string) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.jobs[id], nil
}

func (m *mockJobManager) Transition(_ context.Context, id, to, actorID, ip string, meta map[string]string) (*job.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.transitions = append(m.transitions, id+"->"+to)
	if j, ok := m.jobs[id]; ok {
		j.Status = to
		return j, nil
	}
	j := &job.Job{ID: id, Status: to}
	m.jobs[id] = j
	return j, nil
}

func (m *mockJobManager) AppendEvent(_ context.Context, jobID, eventType string, payload map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, jobID+":"+eventType)
	return nil
}

type mockEmployeeGetter struct {
	emps map[string]*employee.Employee
}

func (m *mockEmployeeGetter) Get(_ context.Context, id string) (*employee.Employee, error) {
	return m.emps[id], nil
}

type mockChatResolver struct {
	chats map[string]string
}

func (m *mockChatResolver) GetChat(jobID string) string {
	return m.chats[jobID]
}

func TestHandleInquiryEventAndCardAction(t *testing.T) {
	ctx := context.Background()
	cmdPusher := &mockCommandPusher{}
	jobMgr := &mockJobManager{jobs: map[string]*job.Job{"JOB-1": {ID: "JOB-1", Status: job.StatusRunning}}}
	empGetter := &mockEmployeeGetter{emps: map[string]*employee.Employee{"EMP-1": {ID: "EMP-1", Name: "小智"}}}
	chatResolver := &mockChatResolver{chats: map[string]string{"JOB-1": "oc_test_chat"}}

	// 创建 feishu 服务带 memory sender
	fsSvc := feishu.NewService(nil)
	memSender := &feishu.MemorySender{}
	fsSvc.Sender = memSender

	svc := inquiry.NewService(cmdPusher, jobMgr, empGetter, fsSvc, chatResolver)
	fsSvc.SetInquiryHandler(svc)

	// 1. 模拟工作站上报 EVENT_TYPE_JOB_INQUIRY
	inqPayload := map[string]any{
		"inquiry_id":  "INQ-1",
		"job_id":      "JOB-1",
		"employee_id": "EMP-1",
		"message":     "AI Agent 请求在生产环境执行 git push",
		"options": []inquiry.InquiryOption{
			{OptionID: "allow-once", Name: "允许本次执行"},
			{OptionID: "deny", Name: "拒绝执行"},
		},
	}
	plBytes, _ := json.Marshal(inqPayload)
	ev := &aiev1.Event{
		JobId:       "JOB-1",
		EmployeeId:  "EMP-1",
		Type:        aiev1.EventType_EVENT_TYPE_JOB_INQUIRY,
		PayloadJson: string(plBytes),
	}

	svc.HandleInquiryEvent(ctx, "WS-1", ev)

	// 检查 Job 状态是否变为 WAITING_APPROVAL
	if len(jobMgr.transitions) == 0 || jobMgr.transitions[0] != "JOB-1->WAITING_APPROVAL" {
		t.Fatalf("预期任务状态切为 WAITING_APPROVAL，实际: %v", jobMgr.transitions)
	}

	// 检查飞书卡片是否发出
	if len(memSender.Sent) == 0 {
		t.Fatalf("预期向飞书发送请示卡片，但没有发出")
	}
	sentCard := memSender.Sent[0]
	if sentCard.ChatID != "oc_test_chat" {
		t.Errorf("卡片接收 chatID 错误，期望 oc_test_chat，实际 %s", sentCard.ChatID)
	}
	if !strings.Contains(sentCard.Content, "INQ-1") {
		t.Errorf("卡片内容未包含 inquiry_id: %s", sentCard.Content)
	}
	if !strings.Contains(sentCard.Content, "允许本次执行") {
		t.Errorf("卡片内容未包含选项名称: %s", sentCard.Content)
	}

	// 2. 模拟用户在飞书卡片中点击「允许本次执行」按钮
	cardActionEv := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Operator: &callback.Operator{
				OpenID: "ou_user_123",
			},
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					"action":      "resolve_inquiry",
					"inquiry_id":  "INQ-1",
					"option_id":   "allow-once",
					"option_name": "允许本次执行",
				},
			},
		},
	}

	resp, err := svc.HandleCardAction(ctx, cardActionEv)
	if err != nil {
		t.Fatalf("HandleCardAction 失败: %v", err)
	}
	if resp == nil || resp.Toast == nil || !strings.Contains(resp.Toast.Content, "允许本次执行") {
		t.Errorf("CardActionTriggerResponse Toast 预期包含选项反馈，实际: %+v", resp)
	}

	// 检查是否向工作站下发了 COMMAND_TYPE_RESOLVE_INQUIRY
	if len(cmdPusher.commands) == 0 {
		t.Fatalf("未向工作站下发指令")
	}
	lastCmd := cmdPusher.commands[len(cmdPusher.commands)-1]
	if lastCmd.Type != aiev1.CommandType_COMMAND_TYPE_RESOLVE_INQUIRY {
		t.Errorf("下发指令类型错误，期望 COMMAND_TYPE_RESOLVE_INQUIRY，实际: %v", lastCmd.Type)
	}
	var cmdPayload struct {
		InquiryID        string `json:"inquiry_id"`
		SelectedOptionID string `json:"selected_option_id"`
	}
	_ = json.Unmarshal([]byte(lastCmd.PayloadJson), &cmdPayload)
	if cmdPayload.InquiryID != "INQ-1" || cmdPayload.SelectedOptionID != "allow-once" {
		t.Errorf("下发指令载荷错误: %+v", cmdPayload)
	}

	// 检查 Job 状态是否恢复为 RUNNING
	if len(jobMgr.transitions) < 2 || jobMgr.transitions[1] != "JOB-1->RUNNING" {
		t.Fatalf("预期任务状态恢复为 RUNNING，实际: %v", jobMgr.transitions)
	}
}

func TestCheckChatReplyNumericAndText(t *testing.T) {
	ctx := context.Background()
	cmdPusher := &mockCommandPusher{}
	jobMgr := &mockJobManager{jobs: map[string]*job.Job{"JOB-2": {ID: "JOB-2", Status: job.StatusRunning}}}
	empGetter := &mockEmployeeGetter{emps: map[string]*employee.Employee{"EMP-2": {ID: "EMP-2", Name: "小爱"}}}
	chatResolver := &mockChatResolver{chats: map[string]string{"JOB-2": "oc_chat_reply"}}

	fsSvc := feishu.NewService(nil)
	memSender := &feishu.MemorySender{}
	fsSvc.Sender = memSender

	svc := inquiry.NewService(cmdPusher, jobMgr, empGetter, fsSvc, chatResolver)
	fsSvc.SetInquiryHandler(svc)

	// 1. 工作站上报请示
	inqPayload := map[string]any{
		"inquiry_id":  "INQ-2",
		"job_id":      "JOB-2",
		"employee_id": "EMP-2",
		"message":     "需要选择发版方案",
		"options": []inquiry.InquiryOption{
			{OptionID: "opt-canary", Name: "灰度发布"},
			{OptionID: "opt-full", Name: "全量发布"},
			{OptionID: "opt-cancel", Name: "取消"},
		},
	}
	plBytes, _ := json.Marshal(inqPayload)
	svc.HandleInquiryEvent(ctx, "WS-2", &aiev1.Event{
		JobId:       "JOB-2",
		EmployeeId:  "EMP-2",
		Type:        aiev1.EventType_EVENT_TYPE_JOB_INQUIRY,
		PayloadJson: string(plBytes),
	})

	// 2. 测试在会话中直接回复数字 "1"
	handled, err := svc.CheckChatReply(ctx, "oc_chat_reply", "1", "ou_tester")
	if err != nil {
		t.Fatalf("CheckChatReply 失败: %v", err)
	}
	if !handled {
		t.Fatalf("数字 1 应该匹配第 1 个选项 [灰度发布]")
	}

	// 验证下发工作站
	if len(cmdPusher.commands) == 0 {
		t.Fatalf("未向工作站下发指令")
	}
	lastCmd := cmdPusher.commands[len(cmdPusher.commands)-1]
	var cmdPayload struct {
		InquiryID        string `json:"inquiry_id"`
		SelectedOptionID string `json:"selected_option_id"`
	}
	_ = json.Unmarshal([]byte(lastCmd.PayloadJson), &cmdPayload)
	if cmdPayload.InquiryID != "INQ-2" || cmdPayload.SelectedOptionID != "opt-canary" {
		t.Errorf("回复 '1' 期望匹配 opt-canary，实际得到: %+v", cmdPayload)
	}

	// 再次回复应该返回 false (因为已经被 resolved)
	handledAgain, _ := svc.CheckChatReply(ctx, "oc_chat_reply", "1", "ou_tester")
	if handledAgain {
		t.Errorf("已处理的请示不应该再次被匹配处理")
	}

	// 3. 测试文本模糊匹配
	svc.HandleInquiryEvent(ctx, "WS-2", &aiev1.Event{
		JobId:       "JOB-2",
		EmployeeId:  "EMP-2",
		Type:        aiev1.EventType_EVENT_TYPE_JOB_INQUIRY,
		PayloadJson: string(plBytes),
	})
	handledText, err := svc.CheckChatReply(ctx, "oc_chat_reply", "我选取消", "ou_tester")
	if err != nil || !handledText {
		t.Fatalf("文本包含 '取消' 应该匹配选项，handled=%v, err=%v", handledText, err)
	}
	lastCmd = cmdPusher.commands[len(cmdPusher.commands)-1]
	_ = json.Unmarshal([]byte(lastCmd.PayloadJson), &cmdPayload)
	if cmdPayload.SelectedOptionID != "opt-cancel" {
		t.Errorf("期望匹配 opt-cancel，实际: %+v", cmdPayload)
	}
}

func TestHandleCardAction_InvalidOrDuplicate(t *testing.T) {
	ctx := context.Background()
	cmdPusher := &mockCommandPusher{}
	jobMgr := &mockJobManager{jobs: map[string]*job.Job{"JOB-3": {ID: "JOB-3", Status: job.StatusRunning}}}
	empGetter := &mockEmployeeGetter{emps: map[string]*employee.Employee{}}
	chatResolver := &mockChatResolver{chats: map[string]string{}}

	svc := inquiry.NewService(cmdPusher, jobMgr, empGetter, nil, chatResolver)

	// 1. 测试 nil 或 非 resolve_inquiry 事件
	resp, err := svc.HandleCardAction(ctx, nil)
	if err != nil || resp == nil {
		t.Fatalf("nil 事件处理异常: %v", err)
	}

	invalidEv := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{"action": "other_action"},
			},
		},
	}
	resp, err = svc.HandleCardAction(ctx, invalidEv)
	if err != nil || resp == nil || resp.Toast != nil {
		t.Fatalf("非 resolve_inquiry 应静默忽略: %+v", resp)
	}

	// 2. 模拟先注册一个请示
	ev := &aiev1.Event{
		JobId: "JOB-3",
		PayloadJson: `{"inquiry_id":"INQ-3","options":[{"optionId":"opt-yes","name":"是"},{"optionId":"opt-no","name":"否"}]}`,
	}
	svc.HandleInquiryEvent(ctx, "WS-3", ev)

	// 首次点击
	cardEv := &callback.CardActionTriggerEvent{
		Event: &callback.CardActionTriggerRequest{
			Action: &callback.CallBackAction{
				Value: map[string]interface{}{
					"action":      "resolve_inquiry",
					"inquiry_id":  "INQ-3",
					"option_id":   "opt-yes",
					"option_name": "是",
				},
			},
		},
	}
	resp, err = svc.HandleCardAction(ctx, cardEv)
	if err != nil || resp.Toast == nil || resp.Toast.Type != "success" {
		t.Fatalf("首次点击应成功: %+v, err: %v", resp, err)
	}

	// 二次点击 (重复处理)：应返回 warning 提示已完成处理
	resp2, err2 := svc.HandleCardAction(ctx, cardEv)
	if err2 != nil {
		t.Fatalf("二次点击不应抛系统级 error: %v", err2)
	}
	if resp2.Toast == nil || resp2.Toast.Type != "warning" || !strings.Contains(resp2.Toast.Content, "已于") {
		t.Fatalf("二次点击期望 warning toast，实际: %+v", resp2.Toast)
	}
}

func TestHandleInquiryEvent_FallbackChatAndNoChat(t *testing.T) {
	ctx := context.Background()
	cmdPusher := &mockCommandPusher{}
	jobMgr := &mockJobManager{jobs: map[string]*job.Job{}}
	empGetter := &mockEmployeeGetter{emps: map[string]*employee.Employee{}}
	chatResolver := &mockChatResolver{chats: map[string]string{}}

	fsSvc := feishu.NewService(nil)
	memSender := &feishu.MemorySender{}
	fsSvc.Sender = memSender

	// 预置员工别名绑定到飞书
	fsSvc.UpsertBinding(feishu.Binding{
		EmployeeID:   "EMP-FALLBACK",
		FeishuOpenID: "ou_bound_user",
	})

	svc := inquiry.NewService(cmdPusher, jobMgr, empGetter, fsSvc, chatResolver)

	// 1. chatResolver 找不到，fallback 到员工绑定的 OpenID
	ev := &aiev1.Event{
		JobId:      "JOB-FB",
		EmployeeId: "EMP-FALLBACK",
		PayloadJson: `{"inquiry_id":"INQ-FB","message":"测试fallback","options":[{"optionId":"1","name":"A"}]}`,
	}
	svc.HandleInquiryEvent(ctx, "WS-FB", ev)

	if len(memSender.Sent) == 0 || memSender.Sent[0].ChatID != "ou_bound_user" {
		t.Fatalf("未成功 fallback 发送到员工绑定的 ou_bound_user: %+v", memSender.Sent)
	}

	// 2. 没有任何绑定的未配置员工，不应 panic 崩溃
	evNoChat := &aiev1.Event{
		JobId:      "JOB-NOCHAT",
		EmployeeId: "EMP-UNKNOWN",
		PayloadJson: `{"inquiry_id":"INQ-NOCHAT","message":"无chat测试"}`,
	}
	svc.HandleInquiryEvent(ctx, "WS-NOCHAT", evNoChat)

	inq := svc.GetInquiry("INQ-NOCHAT")
	if inq == nil || inq.Status != "PENDING" {
		t.Fatalf("无chat场景下请示应正常存储为 PENDING")
	}
}

func TestCheckChatReply_NoMatch(t *testing.T) {
	ctx := context.Background()
	cmdPusher := &mockCommandPusher{}
	jobMgr := &mockJobManager{jobs: map[string]*job.Job{}}
	chatResolver := &mockChatResolver{chats: map[string]string{}}

	svc := inquiry.NewService(cmdPusher, jobMgr, nil, nil, chatResolver)

	// 模拟已存在请示，选项为 [同意, 拒绝]
	ev := &aiev1.Event{
		JobId: "JOB-NOMATCH",
		PayloadJson: `{"inquiry_id":"INQ-NM","chat_id":"oc_test_nm","options":[{"optionId":"yes","name":"同意"},{"optionId":"no","name":"拒绝"}]}`,
	}
	svc.HandleInquiryEvent(ctx, "WS-NM", ev)

	// 发送完全无关的文本
	handled, err := svc.CheckChatReply(ctx, "oc_test_nm", "帮我写一篇年终总结", "ou_user")
	if err != nil {
		t.Fatalf("CheckChatReply 异常: %v", err)
	}
	if handled {
		t.Fatalf("非选项回复不应该被匹配为决策")
	}

	// 空文本
	handledEmpty, _ := svc.CheckChatReply(ctx, "oc_test_nm", "", "ou_user")
	if handledEmpty {
		t.Fatalf("空文本不应匹配")
	}
}

