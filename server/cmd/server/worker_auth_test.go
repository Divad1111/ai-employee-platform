package main

import (
	"context"
	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/employee"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/ai-employee-platform/server/internal/session"
	"testing"
)

func TestResourceEventOwnership(t *testing.T) {
	ctx := context.Background()
	aud := audit.NewMemory()
	bus := eventbus.New(10)
	employees := employee.NewService(employee.NewMemoryStore(), aud, bus)
	e, err := employees.Create(ctx, employee.CreateInput{Name: "test", WorkstationID: "WS-owner"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	jobs := job.NewService(job.NewMemoryStore(), aud, bus)
	j, _, err := jobs.Create(ctx, job.CreateInput{EmployeeID: e.ID, WorkstationID: "WS-owner", WorkspaceID: "workspace", Prompt: "test", IdempotencyKey: "test"}, "user", "")
	if err != nil {
		t.Fatal(err)
	}
	sessions := session.NewService(session.NewMemoryStore(), aud, bus)
	validate := resourceEventValidator(jobs, sessions, employees)
	for _, tc := range []struct {
		name, ws, payload string
		allowed           bool
	}{
		{"nested inquiry", "WS-owner", `{"options":[{"name":"allow"}],"progress":25}`, true},
		{"foreign node", "WS-attacker", `{}`, false},
		{"conflicting job", "WS-owner", `{"job_id":"victim"}`, false},
		{"foreign workspace", "WS-owner", `{"workspace_id":"victim"}`, false},
		{"invalid identifier type", "WS-owner", `{"employee_id":123}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := &aiev1.Event{JobId: j.ID, EmployeeId: e.ID, Type: aiev1.EventType_EVENT_TYPE_JOB_INQUIRY, PayloadJson: tc.payload}
			err := validate(tc.ws, ev)
			if (err == nil) != tc.allowed {
				t.Fatalf("allowed=%v err=%v", tc.allowed, err)
			}
		})
	}
}
