package resume_test

import (
	"testing"

	"github.com/ai-employee-platform/workstation/internal/controlplane/resume"
)

func TestBuildHelloFrame(t *testing.T) {
	h := resume.BuildHello("WS-1", "1.2.3", 42)
	if h.WorkstationId != "WS-1" || h.LastAckedCommandSequence != 42 || h.AgentVersion != "1.2.3" {
		t.Fatal(h)
	}
	frame := resume.HelloFrame(h)
	if frame.GetHello() == nil || frame.GetHello().WorkstationId != "WS-1" {
		t.Fatal(frame)
	}
}
