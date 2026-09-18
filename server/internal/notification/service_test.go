package notification_test

import (
	"context"
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
