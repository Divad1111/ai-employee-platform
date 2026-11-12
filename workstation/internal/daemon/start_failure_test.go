package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	grpcclient "github.com/ai-employee-platform/workstation/internal/controlplane/grpc"
	"github.com/ai-employee-platform/workstation/internal/controlplane/outbox"
	"github.com/ai-employee-platform/workstation/internal/platform"
)

func TestStartJobPreparationFailureReportsJobFailed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		employeeID string
		structured bool
		wantError  string
	}{
		{name: "invalid employee", employeeID: "../invalid", wantError: "非法员工 ID"},
		{name: "outside workspace in JSON", employeeID: "EMP-1", wantError: "工作区超出员工专属目录"},
		{name: "outside workspace in structured command", employeeID: "EMP-1", structured: true, wantError: "工作区超出员工专属目录"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AIE_DATA_DIR", t.TempDir())
			d := New(Options{Paths: platform.Detect()})
			d.wsID = "WS-1"
			sess := &grpcclient.Session{Outbox: &outbox.Dispatcher{Store: d.Outbox}}
			outside := filepath.Join(t.TempDir(), "project")
			cmd := &aiev1.Command{
				Type: aiev1.CommandType_COMMAND_TYPE_START_JOB, JobId: "JOB-1", EmployeeId: tc.employeeID,
			}
			if tc.structured {
				cmd.Structured = &aiev1.Command_StartJob{StartJob: &aiev1.StartJobPayload{WorkspaceId: "W-1", WorkspacePath: outside}}
			} else {
				payload, _ := json.Marshal(map[string]string{"workspace_id": "W-1", "workspace_path": outside})
				cmd.PayloadJson = string(payload)
			}
			err := d.handleCommand(context.Background(), sess, cmd)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected preparation failure %q, got %v", tc.wantError, err)
			}
			events, err := d.Outbox.ListPending(0)
			if err != nil || len(events) != 1 {
				t.Fatalf("expected one queued failure event, got %v, %v", events, err)
			}
			ev := events[0]
			if ev.GetType() != aiev1.EventType_EVENT_TYPE_JOB_FAILED || ev.GetJobId() != "JOB-1" || ev.GetEmployeeId() != tc.employeeID || ev.GetWorkstationId() != "WS-1" {
				t.Fatalf("failure event lost its task identity: %+v", ev)
			}
			var payload map[string]string
			if err := json.Unmarshal([]byte(ev.GetPayloadJson()), &payload); err != nil || !strings.Contains(payload["error"], tc.wantError) {
				t.Fatalf("failure event lost the actual error: %s, %v", ev.GetPayloadJson(), err)
			}
			_, workspaces, sessions, jobs := d.Runtime.Snapshot()
			if workspaces != 0 || sessions != 0 || jobs != 0 {
				t.Fatal("preparation failure started a workspace, session or job")
			}
		})
	}
}
