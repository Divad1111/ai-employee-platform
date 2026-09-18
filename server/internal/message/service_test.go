package message_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/message"
)

func TestEmployeeToEmployeeMessage(t *testing.T) {
	ctx := context.Background()
	svc := message.NewService(message.NewMemoryStore(), audit.NewMemory())
	msg, err := svc.Send(ctx, message.SendInput{
		SenderType: message.TypeEmployee, SenderID: "EMP-1",
		ReceiverType: message.TypeEmployee, ReceiverID: "EMP-2",
		Content: "hello",
	}, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	if msg.SenderType != message.TypeEmployee || msg.ReceiverType != message.TypeEmployee {
		t.Fatal(msg)
	}
	if _, err := svc.Send(ctx, message.SendInput{
		SenderType: "BOT", SenderID: "x", ReceiverType: message.TypeUser, ReceiverID: "u",
	}, "u", ""); err != message.ErrInvalidInput {
		t.Fatal(err)
	}
}
