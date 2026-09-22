package reliability_test

import (
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/reliability"
)

func TestCommandEnqueueAndAck(t *testing.T) {
	s := reliability.NewCommandStore(0)
	cmd, _, err := s.Enqueue("WS-1", aiev1.CommandType_COMMAND_TYPE_START_JOB, "E1", "J1", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Meta.Sequence != 1 {
		t.Fatalf("seq=%d", cmd.Meta.Sequence)
	}
	unacked := s.UnackedAfter("WS-1", 0)
	if len(unacked) != 1 {
		t.Fatalf("len=%d", len(unacked))
	}
	if err := s.Ack("WS-1", cmd.CommandId, 1); err != nil {
		t.Fatal(err)
	}
	if len(s.UnackedAfter("WS-1", 0)) != 0 {
		t.Fatal("ACK 后应无未确认")
	}
	if len(s.UnackedAfter("WS-1", 1)) != 0 {
		t.Fatal("Resume from 1 应为空")
	}
}

func TestEventIdempotentAndStrictOrder(t *testing.T) {
	s := reliability.NewEventStore(0)
	mk := func(id string, seq uint64) *aiev1.Event {
		return &aiev1.Event{
			EventId:       id,
			WorkstationId: "WS-1",
			Meta: &aiev1.EnvelopeMeta{
				MessageId: "m-" + id, Sequence: seq,
				TimestampUnixMs: time.Now().UnixMilli(),
			},
		}
	}
	r1 := s.Accept(mk("e1", 1))
	if !r1.Accepted {
		t.Fatal(r1.Error)
	}
	rDup := s.Accept(mk("e1", 1))
	if !rDup.Accepted || !rDup.Duplicate {
		t.Fatalf("幂等: %+v", rDup)
	}
	rOO := s.Accept(mk("e3", 3))
	if rOO.Accepted {
		t.Fatal("乱序应拒绝")
	}
	r2 := s.Accept(mk("e2", 2))
	if !r2.Accepted {
		t.Fatal(r2.Error)
	}
}

func TestPresenceOffline(t *testing.T) {
	p := reliability.NewPresence(1, 2)
	base := time.Unix(1_700_000_000, 0)
	now := base
	p.SetClock(func() time.Time { return now })
	p.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	now = base.Add(3 * time.Second)
	gone := p.SweepOffline()
	if len(gone) != 1 || gone[0] != "WS-1" {
		t.Fatalf("gone=%v", gone)
	}
	if p.StatusOf("WS-1") != reliability.StatusOffline {
		t.Fatal(p.StatusOf("WS-1"))
	}
	if p.IsSchedulable("WS-1") {
		t.Fatal("Offline 不可调度")
	}
}

func TestCommandEnqueueFull(t *testing.T) {
	s := reliability.NewCommandStore(0)

	// 1. 测试 EnqueueFull 附加结构化载荷 (SyncSkills)
	cmd1, _, err := s.EnqueueFull("WS-1", aiev1.CommandType_COMMAND_TYPE_SYNC_SKILLS, "EMP-1", "", `{"sync":true}`, func(cmd *aiev1.Command) {
		cmd.Structured = &aiev1.Command_SyncSkills{
			SyncSkills: &aiev1.SyncSkillsPayload{
				EmployeeId: "EMP-1",
				Packages: []*aiev1.SkillPackage{
					{Id: "skill.test", CursorName: "sk-test", Version: "1.0.0"},
				},
			},
		}
	})
	if err != nil {
		t.Fatalf("EnqueueFull failed: %v", err)
	}
	if cmd1.Meta.Sequence != 1 {
		t.Fatalf("expected seq 1, got %d", cmd1.Meta.Sequence)
	}
	if cmd1.GetSyncSkills() == nil || len(cmd1.GetSyncSkills().Packages) != 1 {
		t.Fatalf("expected structured SyncSkills payload: %+v", cmd1)
	}

	// 2. 测试 EnqueueFull 递增序列号并附加 StartJob
	cmd2, _, err := s.EnqueueFull("WS-1", aiev1.CommandType_COMMAND_TYPE_START_JOB, "EMP-1", "JOB-1", `{}`, func(cmd *aiev1.Command) {
		cmd.Structured = &aiev1.Command_StartJob{
			StartJob: &aiev1.StartJobPayload{
				WorkflowId: "wf-1",
				Prompt:     "do something",
			},
		}
	})
	if err != nil {
		t.Fatalf("EnqueueFull cmd2 failed: %v", err)
	}
	if cmd2.Meta.Sequence != 2 {
		t.Fatalf("expected seq 2, got %d", cmd2.Meta.Sequence)
	}
	if cmd2.GetStartJob() == nil || cmd2.GetStartJob().WorkflowId != "wf-1" {
		t.Fatalf("expected structured StartJob payload: %+v", cmd2)
	}

	// 3. 验证未确认列表包含两个命令
	unacked := s.UnackedAfter("WS-1", 0)
	if len(unacked) != 2 {
		t.Fatalf("expected 2 unacked commands, got %d", len(unacked))
	}
	if unacked[0].GetSyncSkills() == nil || unacked[1].GetStartJob() == nil {
		t.Fatalf("unacked commands missing structured payloads: %+v", unacked)
	}
}

