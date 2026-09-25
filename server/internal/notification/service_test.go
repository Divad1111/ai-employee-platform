package notification_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/notification"
	"github.com/ai-employee-platform/server/internal/secret"
)

func TestOnJobTerminalSendsFeishu(t *testing.T) {
	ctx := context.Background()
	vault, _ := secret.NewMemoryVault()
	fs := feishu.NewService(vault)
	sender := &feishu.MemorySender{}
	fs.Sender = sender
	bus := eventbus.New(5)
	jobSvc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), bus)
	j, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-1", WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "n1",
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	n := notification.New(fs, bus, jobSvc)
	n.RememberChat(j.ID, "oc_x")
	j.Status = job.StatusSuccess
	j.Result = "done"
	if err := n.OnJobTerminal(ctx, j); err != nil {
		t.Fatal(err)
	}
	if len(sender.Sent) != 1 || sender.Sent[0].ChatID != "oc_x" {
		t.Fatal(sender.Sent)
	}
}

func TestFailedNotifyIncludesErrorWithoutReply(t *testing.T) {
	ctx := context.Background()
	vault, _ := secret.NewMemoryVault()
	fs := feishu.NewService(vault)
	sender := &feishu.MemorySender{}
	fs.Sender = sender
	bus := eventbus.New(5)
	jobSvc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), bus)
	j, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-1", WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "n-fail",
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{job.StatusQueued, job.StatusAssigned, job.StatusStarting} {
		j, err = jobSvc.Transition(ctx, j.ID, st, "u", "", nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	j, err = jobSvc.Transition(ctx, j.ID, job.StatusFailed, "workstation", "", map[string]string{
		"error": "模型未生效，请求 gemini，会话仍是 Auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	n := notification.New(fs, bus, jobSvc)
	n.RememberChat(j.ID, "oc_x")
	if err := n.OnJobTerminal(ctx, j); err != nil {
		t.Fatal(err)
	}
	if len(sender.Sent) != 1 || !strings.Contains(sender.Sent[0].Content, "模型未生效") {
		t.Fatal(sender.Sent)
	}
	if strings.Contains(sender.Sent[0].Content, "status=FAILED") {
		t.Fatal("空回复时不应只用状态占位", sender.Sent[0].Content)
	}
}

func TestFailedNotifyKeepsReplyAndError(t *testing.T) {
	ctx := context.Background()
	vault, _ := secret.NewMemoryVault()
	fs := feishu.NewService(vault)
	sender := &feishu.MemorySender{}
	fs.Sender = sender
	bus := eventbus.New(5)
	jobSvc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), bus)
	j, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-1", WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "n-both",
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range []string{job.StatusQueued, job.StatusAssigned, job.StatusStarting} {
		if _, err = jobSvc.Transition(ctx, j.ID, st, "u", "", nil); err != nil {
			t.Fatal(err)
		}
	}
	if err = jobSvc.SetResult(ctx, j.ID, "部分回复"); err != nil {
		t.Fatal(err)
	}
	j, err = jobSvc.Transition(ctx, j.ID, job.StatusFailed, "workstation", "", map[string]string{
		"error": "配额已用尽",
	})
	if err != nil {
		t.Fatal(err)
	}
	n := notification.New(fs, bus, jobSvc)
	n.RememberChat(j.ID, "oc_x")
	if err := n.OnJobTerminal(ctx, j); err != nil {
		t.Fatal(err)
	}
	body := sender.Sent[0].Content
	if !strings.Contains(body, "部分回复") || !strings.Contains(body, "配额已用尽") {
		t.Fatal(body)
	}
}

func TestTerminalNotifyFallbackToEmployeeBinding(t *testing.T) {
	ctx := context.Background()
	vault, _ := secret.NewMemoryVault()
	fs := feishu.NewService(vault)
	sender := &feishu.MemorySender{}
	fs.Sender = sender
	fs.UpsertBinding(feishu.Binding{EmployeeID: "EMP-BIND", FeishuAlias: "tester", FeishuOpenID: "ou_tester_bind"})
	bus := eventbus.New(5)
	jobSvc := job.NewService(job.NewMemoryStore(), audit.NewMemory(), bus)
	j, _, err := jobSvc.Create(ctx, job.CreateInput{
		EmployeeID: "EMP-BIND", WorkspaceID: "W1", Prompt: "p", IdempotencyKey: "n-fallback",
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	n := notification.New(fs, bus, jobSvc)
	// 不调用 RememberChat，测试自动回退
	j.Status = job.StatusSuccess
	j.Result = "done"
	if err := n.OnJobTerminal(ctx, j); err != nil {
		t.Fatal(err)
	}
	if len(sender.Sent) != 1 || sender.Sent[0].ChatID != "ou_tester_bind" {
		t.Fatalf("未显式绑 chat 时应自动回退到员工 open_id: %+v", sender.Sent)
	}
}

