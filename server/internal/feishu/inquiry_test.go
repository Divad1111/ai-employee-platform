package feishu_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func TestBuildInquiryCardStructure(t *testing.T) {
	options := []feishu.InquiryCardOption{
		{OptionID: "opt-allow", Name: "允许执行"},
		{OptionID: "opt-deny", Name: "拒绝阻断"},
		{OptionID: "opt-custom", Name: "自定义方案"},
	}

	card := feishu.BuildInquiryCard("JOB-999", "测试工程师小李", "INQ-999", "请确认是否删除历史测试镜像", options)
	if card == nil {
		t.Fatal("BuildInquiryCard 返回 nil")
	}

	cardJSON := card.MustJSON()
	if cardJSON == "" || !feishu.IsCardJSON(cardJSON) {
		t.Fatalf("卡片未能序列化为合法的飞书 Card JSON: %s", cardJSON)
	}

	if card.Header == nil || card.Header.Title.Content != "🔔 AI 员工决策/安全请示" {
		t.Errorf("卡片标题错误: %+v", card.Header)
	}

	// 验证包含操作按钮
	hasAction := false
	for _, el := range card.Elements {
		if el["tag"] == "action" {
			hasAction = true
			actions, ok := el["actions"].([]map[string]interface{})
			if !ok || len(actions) != 3 {
				t.Fatalf("actions 格式或数量错误: %+v", el["actions"])
			}
			// 校验按钮属性
			btnAllow := actions[0]
			if btnAllow["type"] != "primary" {
				t.Errorf("允许按钮期望 primary 类型，实际: %v", btnAllow["type"])
			}
			valAllow, _ := btnAllow["value"].(map[string]interface{})
			if valAllow["action"] != "resolve_inquiry" || valAllow["inquiry_id"] != "INQ-999" || valAllow["option_id"] != "opt-allow" {
				t.Errorf("允许按钮 value 数据错误: %+v", valAllow)
			}

			btnDeny := actions[1]
			if btnDeny["type"] != "danger" {
				t.Errorf("拒绝按钮期望 danger 类型，实际: %v", btnDeny["type"])
			}
		}
	}
	if !hasAction {
		t.Fatal("卡片中未找到 tag=action 的交互组件")
	}

	// 测试回执卡片
	resolvedCard := feishu.BuildInquiryResolvedCard("JOB-999", "测试工程师小李", "INQ-999", "允许执行", "ou_admin", "飞书卡片按钮点击")
	if resolvedCard == nil || !feishu.IsCardJSON(resolvedCard.MustJSON()) {
		t.Fatalf("回执卡片生成非法: %+v", resolvedCard)
	}
}

type fakeInquiryHandler struct {
	calledAction bool
	calledReply  bool
	replyMatch   bool
}

func (f *fakeInquiryHandler) HandleCardAction(_ context.Context, _ *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
	f.calledAction = true
	return &callback.CardActionTriggerResponse{
		Toast: &callback.Toast{Type: "success", Content: "ok"},
	}, nil
}

func (f *fakeInquiryHandler) CheckChatReply(_ context.Context, chatID, text, senderOpenID string) (bool, error) {
	f.calledReply = true
	if text == "1" || text == "允许" {
		f.replyMatch = true
		return true, nil
	}
	return false, nil
}

func TestHandleMessageWithInquiryHandler(t *testing.T) {
	ctx := context.Background()
	svc := feishu.NewService(nil)
	memSender := &feishu.MemorySender{}
	svc.Sender = memSender

	fakeInq := &fakeInquiryHandler{}
	svc.SetInquiryHandler(fakeInq)

	// 1. 模拟收到命中选项的文本回复 "1"
	evMatch := feishu.IncomingEvent{
		EventID:      "ev-1",
		ChatID:       "oc_chat_1",
		ChatType:     "p2p",
		SenderOpenID: "ou_user_1",
		Text:         "1",
	}

	jobID, dup, err := svc.HandleMessage(ctx, evMatch)
	if err != nil {
		t.Fatalf("预期无错误，得到: %v", err)
	}
	if dup {
		t.Fatal("不应为重复事件")
	}
	if jobID != "" {
		t.Fatalf("请示回复不应创建新 Job，实际 jobID: %s", jobID)
	}
	if !fakeInq.calledReply || !fakeInq.replyMatch {
		t.Fatal("未调用或未匹配 fakeInquiryHandler.CheckChatReply")
	}
	// 验证没有发送 "请@对应员工执行" 警告卡片
	if len(memSender.Sent) > 0 {
		for _, s := range memSender.Sent {
			if strings.Contains(s.Content, "请@对应员工执行") {
				t.Fatalf("请示回复被错误回复了未识别员工卡片: %s", s.Content)
			}
		}
	}

	// 2. 模拟收到未命中选项的文本回复（例如普通闲聊）
	fakeInq.calledReply = false
	fakeInq.replyMatch = false
	evNoMatch := feishu.IncomingEvent{
		EventID:      "ev-2",
		ChatID:       "oc_chat_1",
		ChatType:     "p2p",
		SenderOpenID: "ou_user_1",
		Text:         "你好世界",
	}

	_, _, err = svc.HandleMessage(ctx, evNoMatch)
	if err != feishu.ErrNoEmployee {
		t.Fatalf("未点名员工且未匹配选项应返回 ErrNoEmployee, 得到: %v", err)
	}
	if !fakeInq.calledReply {
		t.Fatal("未命中的消息应先经过 CheckChatReply 校验")
	}
}
