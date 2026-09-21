package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/providers"
	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

func TestAntigravityProviderName(t *testing.T) {
	p := NewProvider(nil, "antigravity-fake")
	if p.Name() != "antigravity" {
		t.Fatalf("expected 'antigravity', got %s", p.Name())
	}
}

func TestAntigravityProviderDetect(t *testing.T) {
	proc := process.NewManager("antigravity-fake")
	p := NewProvider(proc, "antigravity-fake")
	info, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "antigravity" || info.Path != "antigravity-fake" {
		t.Fatalf("unexpected detect info: %+v", info)
	}
}

func TestAntigravityProviderStartStop_Fake(t *testing.T) {
	proc := process.NewManager("antigravity-fake")
	p := NewProvider(proc, "antigravity-fake")

	sess, err := p.Start(context.Background(), providers.StartSpec{
		SessionID:     "ses-test-1",
		WorkspacePath: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if p.State() != StateReady {
		t.Fatalf("expected state %s, got %s", StateReady, p.State())
	}

	reply, err := sess.Send(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if reply == "" {
		t.Fatal("expected reply, got empty")
	}

	st, err := p.Status(context.Background(), "ses-test-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StateReady {
		t.Fatalf("status state expected %s, got %s", StateReady, st.State)
	}

	if err := p.Stop(context.Background(), "ses-test-1"); err != nil {
		t.Fatal(err)
	}
	if p.State() != StateInstalled {
		t.Fatalf("expected state %s, got %s", StateInstalled, p.State())
	}
}

func TestResolveAgentAPI(t *testing.T) {
	cmd, args, err := resolveAgentAPI("C:\\path\\to\\language_server.exe")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "C:\\path\\to\\language_server.exe" || len(args) != 1 || args[0] != "agentapi" {
		t.Fatalf("unexpected resolve result: cmd=%s args=%v", cmd, args)
	}

	cmd, args, err = resolveAgentAPI("C:\\path\\to\\agentapi.bat")
	if err != nil {
		t.Fatal(err)
	}
	if cmd != "C:\\path\\to\\agentapi.bat" || len(args) != 0 {
		t.Fatalf("unexpected resolve result: cmd=%s args=%v", cmd, args)
	}
}

func TestParseConversationID(t *testing.T) {
	raw := []byte(`{"response":{"newConversation":{"prompt":"hi","conversationId":"test-uuid-123"}}}`)
	cid, err := parseConversationID(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cid != "test-uuid-123" {
		t.Fatalf("expected test-uuid-123, got %s", cid)
	}

	// Mixed stdout
	rawMixed := []byte("Some log\n{\"response\":{\"newConversation\":{\"conversationId\":\"test-uuid-456\"}}}\nDone")
	cid2, err := parseConversationID(rawMixed)
	if err != nil {
		t.Fatal(err)
	}
	if cid2 != "test-uuid-456" {
		t.Fatalf("expected test-uuid-456, got %s", cid2)
	}
}

func TestReadCompletedResponse(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "transcript.jsonl")

	// 模拟未完成（正在调用工具）
	log1 := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"test"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"view_file"}]}
`
	_ = os.WriteFile(logPath, []byte(log1), 0o644)
	reply, done, err := readCompletedResponse(logPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if done {
		t.Fatalf("should not be done yet while tool calls exist: %s", reply)
	}

	// 模拟已完成最终回答
	log2 := log1 + `{"step_index":2,"source":"MODEL","type":"GENERIC","status":"DONE","content":"file content"}
{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"All tasks completed!"}
`
	_ = os.WriteFile(logPath, []byte(log2), 0o644)
	reply, done, err = readCompletedResponse(logPath, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !done || reply != "All tasks completed!" {
		t.Fatalf("expected done with reply 'All tasks completed!', got done=%v reply=%s", done, reply)
	}
}

func TestAntigravityProviderRealIfAvailable(t *testing.T) {
	p := NewProvider(nil, "")
	info, err := p.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Path == "" {
		t.Skip("Antigravity not installed on this machine, skipping real test")
	}
	t.Logf("Antigravity detected at: %s", info.Path)
}

func TestAntigravityProvider_RealExecution(t *testing.T) {
	if os.Getenv("RUN_REAL_ANTIGRAVITY_TEST") != "1" {
		t.Skip("set RUN_REAL_ANTIGRAVITY_TEST=1 to run real Antigravity execution test")
	}
	p := NewProvider(nil, "")
	info, err := p.Detect(context.Background())
	if err != nil || info.Path == "" {
		t.Skip("Antigravity not found")
	}
	wsDir := t.TempDir()
	sess, err := p.Start(context.Background(), providers.StartSpec{
		SessionID:     "ses-real-test",
		WorkspacePath: wsDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Stop(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	reply, err := sess.Send(ctx, []byte("Please reply only with the word PONG"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Real Antigravity Reply: %s", reply)
	if !strings.Contains(strings.ToUpper(reply), "PONG") {
		t.Fatalf("expected PONG in reply, got: %s", reply)
	}
}
