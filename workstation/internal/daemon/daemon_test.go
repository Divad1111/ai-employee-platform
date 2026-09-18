package daemon_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/config"
	"github.com/ai-employee-platform/workstation/internal/daemon"
	"github.com/ai-employee-platform/workstation/internal/ipc"
	"github.com/ai-employee-platform/workstation/internal/platform"
)

func TestDaemonIPCPipeline(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIE_DATA_DIR", root)
	tr := ipc.NewMemoryTransport("daemon-test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := daemon.New(daemon.Options{
		Paths: platform.Detect(), Config: config.Default(),
		Transport: tr, SkipConnect: true,
	})
	go func() { _ = d.Run(ctx) }()
	deadline := time.Now().Add(3 * time.Second)
	for !d.Ready && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !d.Ready {
		t.Fatal("daemon 未 ready")
	}
	cli := &ipc.Client{Transport: tr}
	raw, err := cli.Call(context.Background(), "status", nil)
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	_ = json.Unmarshal(raw, &st)
	if st["ready"] != true {
		t.Fatal(st)
	}

	_, err = cli.Call(context.Background(), "employee.ensure", map[string]string{
		"ID": "EMP-1", "Name": "A", "Provider": "cursor",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.Call(context.Background(), "workspace.ensure", map[string]string{
		"ID": "W1", "EmployeeID": "EMP-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cli.Call(context.Background(), "session.start", map[string]string{
		"ID": "SES-1", "EmployeeID": "EMP-1", "WorkspaceID": "W1", "Provider": "cursor",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = cli.Call(context.Background(), "job.run", map[string]string{
		"ID": "JOB-1", "EmployeeID": "EMP-1", "SessionID": "SES-1", "Prompt": "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	var job map[string]any
	_ = json.Unmarshal(raw, &job)
	if job["Status"] != "SUCCESS" && job["status"] != "SUCCESS" {
		// json tags not on runtime.Job - fields are exported Status
		if s, ok := job["Status"].(string); !ok || s != "SUCCESS" {
			t.Fatalf("job=%v", job)
		}
	}
	doc, err := cli.Call(context.Background(), "doctor", nil)
	if err != nil || len(doc) == 0 {
		t.Fatal(err)
	}
	cancel()
}
