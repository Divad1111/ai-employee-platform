package process_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

func TestWhitelistRejectsArbitrary(t *testing.T) {
	m := process.NewManager("cursor.exe")
	m.SetRunner(&process.FakeRunner{})
	_, err := m.Start(context.Background(), process.Spec{ID: "1", Binary: "cmd.exe", Args: []string{"/c", "dir"}})
	if err == nil {
		t.Fatal("应拒绝非白名单")
	}
}

func TestStartStopInspect(t *testing.T) {
	m := process.NewManager("cursor-fake")
	fake := &process.FakeRunner{}
	m.SetRunner(fake)
	info, err := m.Start(context.Background(), process.Spec{ID: "s1", Name: "cursor", Binary: "cursor-fake"})
	if err != nil {
		t.Fatal(err)
	}
	if info.PID == 0 || !info.Running {
		t.Fatal(info)
	}
	// FakeRunner 的 PID 非真实进程，Inspect 的 signalAlive 探测会误判；以 Manager 停机结果为准
	if err := m.Stop(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Inspect("s1")
	if got.Running {
		t.Fatal("停止后应非 Running")
	}
}
