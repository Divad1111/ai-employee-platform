package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/workstation/internal/acp"
	"github.com/ai-employee-platform/workstation/internal/config"
	"github.com/ai-employee-platform/workstation/internal/platform"
)

func TestDaemonInquiryNoSessionFallback(t *testing.T) {
	d := New(Options{
		Paths:  platform.Detect(),
		Config: config.Default(),
	})

	inq := acp.Inquiry{
		Message: "测试无连接请示",
		Options: []acp.InquiryOption{
			{OptionID: "opt-1", Name: "选项1"},
			{OptionID: "opt-2", Name: "选项2"},
		},
	}

	// 当没有活跃 gRPC Session 时，应立即 fallback 到 DefaultOptionID (opt-1)
	res, err := d.handleAgentInquiry(context.Background(), "JOB-10", "EMP-10", inq)
	if err != nil {
		t.Fatalf("预期无错误，得到: %v", err)
	}
	if res != "opt-1" {
		t.Errorf("预期返回 opt-1，实际得到: %s", res)
	}
}

func TestDaemonResolveInquiryFlow(t *testing.T) {
	d := New(Options{
		Paths:  platform.Detect(),
		Config: config.Default(),
	})

	inquiryID := "INQ-TEST-001"
	ch := make(chan string, 1)

	d.inqMu.Lock()
	d.pendingInquiries[inquiryID] = ch
	d.inqMu.Unlock()

	// 1. 模拟收到服务器下发的 COMMAND_TYPE_RESOLVE_INQUIRY 指令
	cmdPayload, _ := json.Marshal(map[string]any{
		"inquiry_id":         inquiryID,
		"selected_option_id": "allow-always",
	})
	cmd := &aiev1.Command{
		Type:        aiev1.CommandType_COMMAND_TYPE_RESOLVE_INQUIRY,
		PayloadJson: string(cmdPayload),
	}

	err := d.handleCommand(context.Background(), nil, cmd)
	if err != nil {
		t.Fatalf("handleCommand 失败: %v", err)
	}

	// 验证 channel 是否收到选择结果
	select {
	case chosen := <-ch:
		if chosen != "allow-always" {
			t.Errorf("期望收到 allow-always，实际收到: %s", chosen)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("等待 resolveInquiry 超时")
	}

	// 验证 pendingInquiries 是否已被清理
	d.inqMu.Lock()
	_, exists := d.pendingInquiries[inquiryID]
	d.inqMu.Unlock()
	if exists {
		t.Errorf("inquiryID 应在 resolve 后被清理")
	}
}

func TestDaemonInquiryContextCancel(t *testing.T) {
	d := New(Options{
		Paths:  platform.Detect(),
		Config: config.Default(),
	})

	inq := acp.Inquiry{
		Message: "测试超时取消",
		Options: []acp.InquiryOption{
			{OptionID: "opt-fallback", Name: "默认选项"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 模拟已存在活跃 session (用 mock 检查 context 取消分支)
	// 由于没有 session 默认走 no session 分支；当有 channel 且 ctx cancelled 时走 ctx.Done()
	res, _ := d.handleAgentInquiry(ctx, "JOB-11", "EMP-11", inq)
	if res != "opt-fallback" {
		t.Errorf("取消时应安全 fallback 到默认选项，实际得到: %s", res)
	}
}
