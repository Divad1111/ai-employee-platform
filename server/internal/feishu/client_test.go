package feishu_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/secret"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func TestService_SDKIntegration(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/open-apis/auth/v3/tenant_access_token/internal" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":                0,
				"msg":                 "ok",
				"tenant_access_token": "t-token-123456",
				"expire":              7200,
			})
			return
		}
		if r.URL.Path == "/open-apis/im/v1/messages" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"msg":  "ok",
				"data": map[string]any{
					"message_id":  "om_test_msg_001",
					"chat_id":     "oc_test_chat_001",
					"create_time": "1726700000",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	vault, _ := secret.NewMemoryVault()
	secRef, _ := vault.Put("feishu.app_secret", "test_sec")

	svc := feishu.NewService(vault)
	larkClient := lark.NewClient("test_app", "test_sec", lark.WithOpenBaseUrl(ts.URL))
	svc.SetLarkClient(larkClient)

	ctx := context.Background()

	// 1. 未配置
	s1 := svc.Status(ctx, true)
	if s1.Configured {
		t.Errorf("expected Configured=false when unconfigured")
	}

	// 2. 配置但未启用
	svc.SetConfig(feishu.Config{
		AppID:        "test_app",
		AppSecretRef: secRef.ID,
		Enabled:      false,
	})
	s2 := svc.Status(ctx, true)
	if !s2.Configured || s2.Enabled || s2.Connected {
		t.Errorf("unexpected s2: %+v", s2)
	}

	// 3. 启用并正常连接（状态自检）
	svc.SetConfig(feishu.Config{
		AppID:        "test_app",
		AppSecretRef: secRef.ID,
		Enabled:      true,
	})
	svc.SetLarkClient(larkClient) // 注入测试 mock client

	s3 := svc.Status(ctx, true)
	if !s3.Connected {
		t.Errorf("expected Connected=true, got: %+v", s3)
	}

	// 4. 发送测试消息
	sendRes, err := svc.TestSendMessage(ctx, larkim.CreateMessageV1ReceiveIDTypeOpenId, "ou_target_123", "Hello Feishu")
	if err != nil {
		t.Fatalf("TestSendMessage failed: %v", err)
	}
	if sendRes.MessageID != "om_test_msg_001" {
		t.Errorf("unexpected message_id: %s", sendRes.MessageID)
	}

	// 5. SDKSender 回复测试
	sender := feishu.NewSDKSender(svc)
	err = sender.Send(ctx, feishu.Reply{ChatID: "oc_test_chat_001", Content: "Job Completed"})
	if err != nil {
		t.Fatalf("SDKSender.Send failed: %v", err)
	}
}

func TestWSGateway_Lifecycle(t *testing.T) {
	gw := feishu.NewWSGateway()
	st := gw.Status()
	if st.State != "DISCONNECTED" {
		t.Errorf("expected DISCONNECTED, got %s", st.State)
	}

	_ = gw.Restart(context.Background())
	gw.Stop()
	st = gw.Status()
	if st.State != "DISCONNECTED" {
		t.Errorf("expected DISCONNECTED after Stop, got %s", st.State)
	}
}
