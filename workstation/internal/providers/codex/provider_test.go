package codex_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/providers/codex"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

func TestCodexProviderStart(t *testing.T) {
	proc := process.NewManager("codex-fake")
	proc.SetRunner(&process.FakeRunner{})
	p := codex.NewProvider(proc, "codex-fake")
	as, err := p.Start(context.Background(), providers.StartSpec{SessionID: "S1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := as.Send(context.Background(), []byte("p")); err != nil {
		t.Fatal(err)
	}
	_ = p.Stop(context.Background(), "S1")
}
