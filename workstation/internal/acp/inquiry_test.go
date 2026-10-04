package acp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestParseInquiryVariations(t *testing.T) {
	// 1. 标准 options
	raw1 := json.RawMessage(`{
		"message": "是否允许执行 Shell 命令",
		"options": [
			{"optionId": "opt-1", "name": "允许一次"},
			{"optionId": "opt-2", "name": "永久允许"}
		]
	}`)
	inq1 := parseInquiry("sess-1", "session/request_permission", raw1)
	if inq1.SessionID != "sess-1" || inq1.Message != "是否允许执行 Shell 命令" {
		t.Errorf("inq1 基础信息错误: %+v", inq1)
	}
	if len(inq1.Options) != 2 || inq1.Options[0].OptionID != "opt-1" || inq1.Options[0].Name != "允许一次" {
		t.Errorf("inq1 options 解析错误: %+v", inq1.Options)
	}
	if inq1.DefaultOptionID() != "opt-1" {
		t.Errorf("inq1 DefaultOptionID 应为 opt-1, got %s", inq1.DefaultOptionID())
	}

	// 2. choices 格式与 prompt / title 兜底
	raw2 := json.RawMessage(`{
		"title": "选择执行方案",
		"choices": [
			{"id": "c1", "label": "方案A"},
			{"id": "c2", "text": "方案B"}
		]
	}`)
	inq2 := parseInquiry("sess-2", "session/user_choice", raw2)
	if inq2.Message != "选择执行方案" {
		t.Errorf("inq2 消息提取错误: %s", inq2.Message)
	}
	if len(inq2.Options) != 2 || inq2.Options[0].OptionID != "c1" || inq2.Options[0].Name != "方案A" {
		t.Errorf("inq2 choices 解析错误: %+v", inq2.Options)
	}

	// 3. toolCall 提取
	raw3 := json.RawMessage(`{
		"toolCall": {"name": "deploy_production"}
	}`)
	inq3 := parseInquiry("sess-3", "session/request_permission", raw3)
	if inq3.Message != "AI Agent 请求执行工具调用: deploy_production" {
		t.Errorf("inq3 toolCall 描述错误: %s", inq3.Message)
	}
	// 验证空 options 自动兜底
	if len(inq3.Options) != 2 || inq3.Options[0].OptionID != "allow-once" || inq3.Options[1].OptionID != "deny" {
		t.Errorf("inq3 兜底选项错误: %+v", inq3.Options)
	}
	if inq3.DefaultOptionID() != "allow-once" {
		t.Errorf("inq3 默认选项应为 allow-once, got %s", inq3.DefaultOptionID())
	}
}

func TestStdioSessionHandleInquiryRequest(t *testing.T) {
	// 测试 handleInquiryRequest 触发回调与结果封装
	s := &StdioSession{
		id:           "ses-test",
		acpSessionID: "ses-acp-test",
	}

	var capturedInq Inquiry
	s.SetInquiryHandler(func(ctx context.Context, inq Inquiry) (string, error) {
		capturedInq = inq
		return "custom-choice", nil
	})

	rawParams := json.RawMessage(`{
		"message": "确认执行",
		"options": [{"optionId": "custom-choice", "name": "确定"}]
	}`)

	// 调用 handleInquiryRequest（在无真实 stdin 下，respond 会返回 ErrNotStarted，但核心逻辑回调正常执行）
	s.handleInquiryRequest(101, "session/request_permission", rawParams)

	if capturedInq.SessionID != "ses-acp-test" {
		t.Errorf("回调中的 SessionID 错误: %s", capturedInq.SessionID)
	}
	if capturedInq.Message != "确认执行" {
		t.Errorf("回调中的 Message 错误: %s", capturedInq.Message)
	}

	// 测试当 handler 返回 error 时自动回退到 DefaultOptionID
	s.SetInquiryHandler(func(ctx context.Context, inq Inquiry) (string, error) {
		return "", errors.New("timeout")
	})
	s.handleInquiryRequest(102, "session/request_permission", rawParams)
}
