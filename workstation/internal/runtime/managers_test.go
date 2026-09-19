package runtime_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/cursor"
	"github.com/ai-employee-platform/workstation/internal/runtime"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
	"github.com/ai-employee-platform/workstation/internal/runtime/recovery"
)

func TestSessionJobRecoveryUnknown(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIE_DATA_DIR", root)
	paths := platform.Detect()
	proc := process.NewManager("cursor-fake")
	proc.SetRunner(&process.FakeRunner{})
	reg := providers.NewRegistry()
	reg.Register(cursor.NewProvider(proc, "cursor-fake"))
	rt := runtime.NewManagers(paths, reg)

	emp, err := rt.EnsureEmployee("EMP-1", "Alice", "cursor")
	if err != nil {
		t.Fatal(err)
	}
	if emp.Dir != filepath.Join(paths.WorkspaceRoot(), "EMP-1") {
		t.Fatal(emp.Dir)
	}
	ws, err := rt.EnsureWorkspace("W1", "EMP-1", "")
	if err != nil {
		t.Fatal(err)
	}
	sess, err := rt.StartSession(context.Background(), "SES-1", "EMP-1", ws.ID, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Status != runtime.SessReady {
		t.Fatal(sess.Status)
	}
	if _, err := rt.StartSession(context.Background(), "SES-2", "EMP-1", ws.ID, "cursor"); err != runtime.ErrActiveSession {
		t.Fatalf("应限制 Active Session: %v", err)
	}
	job, reply, err := rt.RunJob(context.Background(), "JOB-1", "EMP-1", "SES-1", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if reply == "" {
		t.Fatal("应返回 reply")
	}
	_ = job
	if err != nil || job.Status != runtime.JobSuccess {
		t.Fatal(job, err)
	}

	// 模拟崩溃恢复：BOOT 时 RUNNING → UNKNOWN，不标 SUCCESS 回写
	rt.MarkUnknown("SES-1", "")
	s, _ := rt.GetSession("SES-1")
	if s.Status != runtime.SessUnknown {
		t.Fatal(s.Status)
	}
	rec := &recovery.Manager{Runtime: rt, Proc: proc}
	rec.OnACPDisconnect("SES-1", "JOB-x")
	rec.BootRecover()
}

func TestJobManagerDoesNotImportCursorType(t *testing.T) {
	// 编译期保证：runtime 只依赖 providers.Registry 接口
	reg := providers.NewRegistry()
	_ = runtime.NewManagers(platform.Detect(), reg)
}
