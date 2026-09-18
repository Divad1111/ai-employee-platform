package security_test

import (
	"testing"

	"github.com/ai-employee-platform/workstation/internal/security"
)

func TestGateDefaultDeny(t *testing.T) {
	g := security.NewGate()
	if _, err := g.Check("git.push"); err != security.ErrUnknownAct {
		t.Fatal(err)
	}
	if security.HasShellBypass() {
		t.Fatal("不得存在 shell 旁路")
	}
}

func TestGateAllowAskDeny(t *testing.T) {
	g := security.NewGate()
	g.Sync([]security.Rule{
		{Action: "git.status", Effect: security.EffectAllow},
		{Action: "git.push", Effect: security.EffectAsk, Critical: true},
		{Action: "system.shutdown", Effect: security.EffectDeny},
	})
	if _, err := g.Check("git.status"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Check("git.push"); err != security.ErrNeedAsk {
		t.Fatal(err)
	}
	if _, err := g.Check("system.shutdown"); err != security.ErrDenied {
		t.Fatal(err)
	}
}
