package recovery_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/cursor"
	"github.com/ai-employee-platform/workstation/internal/runtime"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
	"github.com/ai-employee-platform/workstation/internal/runtime/recovery"
)

func TestProcessCrashMarksUnknownNotSuccess(t *testing.T) {
	t.Setenv("AIE_DATA_DIR", t.TempDir())
	proc := process.NewManager("cursor-fake")
	fake := &process.FakeRunner{}
	proc.SetRunner(fake)
	reg := providers.NewRegistry()
	reg.Register(cursor.NewProvider(proc, "cursor-fake"))
	rt := runtime.NewManagers(platform.Detect(), reg)
	_, _ = rt.EnsureEmployee("E1", "n", "cursor")
	ws, _ := rt.EnsureWorkspace("W1", "E1", "")
	_, err := rt.StartSession(context.Background(), "S1", "E1", ws.ID, "cursor")
	if err != nil {
		t.Fatal(err)
	}
	// 启动一个受控进程再杀掉
	_, _ = proc.Start(context.Background(), process.Spec{ID: "S1", Binary: "cursor-fake"})
	_ = proc.Stop(context.Background(), "S1")

	rec := &recovery.Manager{Runtime: rt, Proc: proc}
	crashed, err := rec.CheckProcessCrash(context.Background(), "S1", "J1")
	if err != nil || !crashed {
		t.Fatalf("crashed=%v err=%v", crashed, err)
	}
	s, _ := rt.GetSession("S1")
	if s.Status != runtime.SessUnknown {
		t.Fatal(s.Status)
	}
	rec.OnNetworkUnavailable("S1", "J1")
}
