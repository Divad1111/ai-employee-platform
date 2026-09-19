package cursor_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/cursor"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

func TestCursorProviderLifecycle(t *testing.T) {
	proc := process.NewManager("agent-fake")
	proc.SetRunner(&process.FakeRunner{})
	// BinaryOverride=agent-fake → 走 Fake ACP，不拉 GUI / 不 spawn 真进程
	p := cursor.NewProvider(proc, "agent-fake")
	info, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "cursor" {
		t.Fatal(info)
	}
	as, err := p.Start(context.Background(), providers.StartSpec{SessionID: "SES-c", EmployeeID: "E1"})
	if err != nil {
		t.Fatal(err)
	}
	if p.State() != cursor.StateReady {
		t.Fatal(p.State())
	}
	if _, err := as.Send(context.Background(), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(context.Background(), "SES-c"); err != nil {
		t.Fatal(err)
	}
}

func TestCursorRejectsGUIBinary(t *testing.T) {
	proc := process.NewManager("/Applications/Cursor.app/Contents/MacOS/Cursor")
	p := cursor.NewProvider(proc, "/Applications/Cursor.app/Contents/MacOS/Cursor")
	_, err := p.Start(context.Background(), providers.StartSpec{SessionID: "SES-gui"})
	if err == nil {
		t.Fatal("应拒绝 Cursor GUI 二进制")
	}
}

func TestCursorInstallerDetectOnly(t *testing.T) {
	inst := &cursor.Installer{Path: ""}
	if err := inst.Install(context.Background(), providers.InstallSpec{}); err == nil {
		t.Fatal("V1 Install 应拒绝 Registry 下载")
	}
	_, _ = inst.Detect(context.Background())
}
