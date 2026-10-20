package runtime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
)

// Completion is controlled separately from Stop to reproduce a late reply
// after cancellation, including a replacement session with the same ID.
type lifecycleAgent struct {
	entered chan struct{}
	finish  chan error
}

func newLifecycleAgent() *lifecycleAgent {
	return &lifecycleAgent{entered: make(chan struct{}), finish: make(chan error, 1)}
}
func (*lifecycleAgent) Start(context.Context) error { return nil }
func (*lifecycleAgent) Stop(context.Context) error  { return nil }
func (a *lifecycleAgent) Send(ctx context.Context, _ []byte) (string, error) {
	close(a.entered)
	select {
	case err := <-a.finish:
		return "reply", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type lifecycleProvider struct {
	providers.AgentProvider
	agents []*lifecycleAgent
}

func (*lifecycleProvider) Name() string                       { return "lifecycle-test" }
func (*lifecycleProvider) Stop(context.Context, string) error { return nil }
func (p *lifecycleProvider) Start(context.Context, providers.StartSpec) (providers.AgentSession, error) {
	a := p.agents[0]
	p.agents = p.agents[1:]
	return a, nil
}

func awaitLifecycle(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for controlled turn")
	}
}

func TestCancelledTurnCannotResurrectOrOverwriteSession(t *testing.T) {
	for _, replace := range []bool{false, true} {
		for _, result := range []error{nil, context.Canceled} {
			t.Run(fmt.Sprintf("replacement=%t/result=%v", replace, result), func(t *testing.T) {
				t.Setenv("AIE_DATA_DIR", t.TempDir())
				oldAgent, newAgent := newLifecycleAgent(), newLifecycleAgent()
				reg := providers.NewRegistry()
				reg.Register(&lifecycleProvider{agents: []*lifecycleAgent{oldAgent, newAgent}})
				m := NewManagers(platform.Detect(), reg)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				start := func() {
					t.Helper()
					if _, err := m.StartSession(ctx, "session", "employee", "", "lifecycle-test"); err != nil {
						t.Fatal(err)
					}
				}
				run := func(id string) <-chan struct{} {
					done := make(chan struct{})
					go func() {
						defer close(done)
						_, _, _ = m.RunJob(ctx, id, "employee", "session", "read file")
					}()
					return done
				}
				start()
				oldDone := run("old-job")
				awaitLifecycle(t, oldAgent.entered)
				if err := m.CancelJob(ctx, "old-job"); err != nil {
					t.Fatal(err)
				}
				var newDone <-chan struct{}
				if replace {
					start()
					newDone = run("new-job")
					awaitLifecycle(t, newAgent.entered)
				}
				oldAgent.finish <- result
				awaitLifecycle(t, oldDone)
				s, _ := m.GetSession("session")
				if !replace {
					if s.Status != SessStopped || m.FindActiveSession("employee") != nil || m.SessionReady("session") {
						t.Fatalf("cancelled session resurrected: %+v", s)
					}
					start()
					newDone = run("new-job")
					awaitLifecycle(t, newAgent.entered)
				} else if s.Status != SessBusy {
					t.Fatalf("old completion overwrote replacement: %+v", s)
				}
				newAgent.finish <- nil
				awaitLifecycle(t, newDone)
				j, _ := m.GetJob("new-job")
				if j.Status != JobSuccess || !m.SessionReady("session") {
					t.Fatalf("next job failed: %+v", j)
				}
			})
		}
	}
}

func TestRunJobRejectsUnreadySessionWithoutChangingLifecycle(t *testing.T) {
	for _, status := range []string{SessStarting, SessStopped, SessUnknown, SessBusy, SessReady} {
		t.Run(status, func(t *testing.T) {
			m := NewManagers(platform.Detect(), providers.NewRegistry())
			m.sessions["session"] = &Session{ID: "session", EmployeeID: "employee", Status: status}
			if status != SessReady { // READY with no bound agent reproduces the reported failure.
				m.agentSess["session"] = newLifecycleAgent()
			}
			j, _, err := m.RunJob(context.Background(), "job", "employee", "session", "read file")
			if err == nil || j.Status != JobFailed {
				t.Fatalf("unready job accepted: %+v, %v", j, err)
			}
			if s, _ := m.GetSession("session"); s.Status != status {
				t.Fatalf("invalid admission changed lifecycle: %+v", s)
			}
		})
	}
}

func TestOrdinaryTurnErrorLeavesLiveSessionReusable(t *testing.T) {
	m := NewManagers(platform.Detect(), providers.NewRegistry())
	a := newLifecycleAgent()
	a.finish <- errors.New("ordinary turn failure")
	m.sessions["session"] = &Session{ID: "session", EmployeeID: "employee", Status: SessReady}
	m.agentSess["session"] = a
	j, _, err := m.RunJob(context.Background(), "job", "employee", "session", "read file")
	if err == nil || j.Status != JobFailed || !m.SessionReady("session") {
		t.Fatalf("ordinary failure broke live session: %+v, %v", j, err)
	}
}

type closedLifecycleAgent struct{ *lifecycleAgent }

func (*closedLifecycleAgent) Ready() bool { return false }

func TestClosedEngineIsNotReusable(t *testing.T) {
	m := NewManagers(platform.Detect(), providers.NewRegistry())
	m.sessions["session"] = &Session{ID: "session", EmployeeID: "employee", Status: SessReady}
	m.agentSess["session"] = &closedLifecycleAgent{newLifecycleAgent()}
	if m.SessionReady("session") {
		t.Fatal("closed engine accepted for reuse")
	}
	j, _, err := m.RunJob(context.Background(), "job", "employee", "session", "read file")
	if err == nil || j.Status != JobFailed {
		t.Fatalf("closed engine accepted job: %+v, %v", j, err)
	}
}
