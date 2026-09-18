package cursor_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/cursor"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

func TestCursorProviderLifecycle(t *testing.T) {
	proc := process.NewManager("cursor-fake")
	proc.SetRunner(&process.FakeRunner{})
	p := cursor.NewProvider(proc, "cursor-fake")
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
	if err := as.Send(context.Background(), []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop(context.Background(), "SES-c"); err != nil {
		t.Fatal(err)
	}
}

func TestCursorInstallerDetectOnly(t *testing.T) {
	inst := &cursor.Installer{Path: ""}
	if err := inst.Install(context.Background(), providers.InstallSpec{}); err == nil {
		t.Fatal("V1 Install 应拒绝 Registry 下载")
	}
	_, _ = inst.Detect(context.Background())
}
