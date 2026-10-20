package scheduler_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/certca"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/reliability"
	"github.com/ai-employee-platform/server/internal/scheduler"
	"github.com/ai-employee-platform/server/internal/workstation"
)

func TestSchedulerCarriesInputManifestAndTypedSessionSettings(t *testing.T) {
	ctx := context.Background()
	aud, bus := audit.NewMemory(), eventbus.New(20)
	emps := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, _ := emps.Create(ctx, employee.CreateInput{Name: "A", WorkstationID: "WS-1", WorkspaceID: "W1"}, "user", "")
	e, _ = emps.StartNewSession(ctx, e.ID, "user", "")
	jobs := job.NewService(job.NewMemoryStore(), aud, bus)
	f := input.File{SHA256: strings.Repeat("a", 64), Name: "image.png", MIMEType: "image/png", Size: 100}
	j, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkspaceID: "W1", IdempotencyKey: "with-input", Prompt: "inspect", InputAttachments: []input.File{f}}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := certca.NewDevAuthority()
	presence := reliability.NewPresence(5, 15)
	presence.Touch("WS-1", "v", 0, 0, 0, 0, 0)
	ws := workstation.NewService(ca, presence, nil)
	pusher := &fakePusher{}
	s := scheduler.New(jobs, emps, ws, presence, pusher)
	if _, err := s.ScheduleJob(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		InputAttachments    []input.File `json:"input_attachments"`
		SessionGeneration   int64        `json:"session_generation"`
		ResetSessionContext bool         `json:"reset_session_context"`
	}
	if err := json.Unmarshal([]byte(pusher.cmds[0].PayloadJson), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.InputAttachments) != 1 || payload.InputAttachments[0] != f || payload.SessionGeneration != 1 || !payload.ResetSessionContext {
		t.Fatalf("start payload lost input/session settings: %+v", payload)
	}
}
