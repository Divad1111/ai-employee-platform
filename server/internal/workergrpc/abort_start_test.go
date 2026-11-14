package workergrpc_test

import (
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/workergrpc"
)

func TestAbortedStartPreservesResumeOrderWithoutReplayingPayload(t *testing.T) {
	s := workergrpc.NewServer(workergrpc.Options{})
	start, err := s.PushCommandFull("WS", aiev1.CommandType_COMMAND_TYPE_START_JOB, "EMP", "failed", `{"prompt":"never replay"}`, func(c *aiev1.Command) {
		c.Structured = &aiev1.Command_StartJob{StartJob: &aiev1.StartJobPayload{Prompt: "never replay", McpServers: []*aiev1.MCPServerSpec{{Headers: map[string]string{"Authorization": "test-token"}}}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.PushCommand("WS", aiev1.CommandType_COMMAND_TYPE_START_JOB, "other", "other-job", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AbortJobStart("WS", "EMP", "failed", "send failed"); err != nil {
		t.Fatal(err)
	}
	commands := s.Commands.UnackedAfter("WS", 0)
	if len(commands) != 3 {
		t.Fatalf("unexpected resume commands: %+v", commands)
	}
	for i, cmd := range commands {
		if cmd.GetMeta().GetSequence() != uint64(i+1) {
			t.Fatal("aborting created a sequence gap")
		}
	}
	if commands[0].GetType() != aiev1.CommandType_COMMAND_TYPE_STOP_JOB || commands[0].GetStartJob() != nil || commands[0].GetCommandId() != start.GetCommandId() {
		t.Fatal("failed start payload survived rollback")
	}
	if commands[1] != other || commands[2].GetType() != aiev1.CommandType_COMMAND_TYPE_STOP_JOB || commands[2].GetJobId() != "failed" {
		t.Fatal("rollback changed another task or omitted the fresh stop")
	}
	if start.GetType() != aiev1.CommandType_COMMAND_TYPE_START_JOB || start.GetStartJob() == nil {
		t.Fatal("rollback mutated an in-flight command snapshot")
	}
	if err := s.Commands.Ack("WS", start.GetCommandId(), start.GetMeta().GetSequence()); err != nil {
		t.Fatalf("late acknowledgement was rejected: %v", err)
	}
	if s.StartCommandUnacked("WS", "failed") {
		t.Fatal("failed start can be resumed")
	}
}
