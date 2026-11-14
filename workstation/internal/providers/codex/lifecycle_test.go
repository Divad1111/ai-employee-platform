package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/acp"
)

func TestJobRequiresCompletedTurnAndFinalResult(t *testing.T) {
	for _, test := range []struct {
		status, final string
		success       bool
	}{
		{"interrupted", "", false},
		{"completed", "", false},
		{"completed", `{"status":"completed","kind":"action","reply":"Merged","evidence":"I merged it"}`, false},
		{"completed", `{"status":"incomplete","reply":"Only inspected","evidence":"Approval denied"}`, false},
		{"completed", `{"status":"completed","reply":"Merged"}`, false},
		{"completed", `{"status":"completed","kind":"action","reply":"Merged","evidence":"r123 in test"}`, true},
	} {
		s := newAppServerSession("session", "unused", t.TempDir(), "")
		s.ready, s.threadID = true, "thread"
		peer := &stopRPCWriter{s: s, ack: true, written: make(chan struct{})}
		s.stdin = peer
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		done := make(chan error, 1)
		go func() { _, err := s.Send(ctx, []byte("merge")); done <- err }()
		select {
		case <-peer.written:
		case <-ctx.Done():
			t.Fatal("turn never started")
		}
		notify(s, "item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "phase": "commentary", "text": "Inspecting repository"}})
		ev := <-s.Events()
		if ev.Type != "agent_activity" || string(ev.Payload) != "Inspecting repository" {
			t.Fatal(ev)
		}
		if test.success {
			notify(s, "item/completed", map[string]any{"item": map[string]any{"type": "commandExecution", "status": "completed", "exitCode": 0}})
		}
		if test.final != "" {
			notify(s, "item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "phase": "final_answer", "text": test.final}})
		}
		notify(s, "turn/completed", map[string]any{"turn": map[string]string{"status": test.status}})
		err := <-done
		cancel()
		if (err == nil) != test.success {
			t.Fatalf("%s %s: %v", test.status, test.final, err)
		}
	}
}

type inquiryResponse struct{ bytes.Buffer }

func (*inquiryResponse) Close() error { return nil }

func TestWrappedSessionForwardsApprovalAndQuestion(t *testing.T) {
	s := newAppServerSession("session", "unused", t.TempDir(), "")
	var wire inquiryResponse
	s.stdin = &wire
	wrapper := &agentSession{acp: s}
	var requests []string
	wrapper.SetInquiryHandler(func(_ context.Context, q acp.Inquiry) (string, error) {
		requests = append(requests, q.Message)
		if q.Method == "item/tool/requestUserInput" {
			if q.AllowText {
				return "text:my answer", nil
			}
			return "choice-2", nil
		}
		return "accept", nil
	})
	s.handleApprovalRequest(rpcMsg{ID: 42, Method: "item/commandExecution/requestApproval", Params: json.RawMessage(`{"command":"svn status","availableDecisions":["accept","cancel"]}`)})
	if !bytes.Contains(wire.Bytes(), []byte(`"decision":"accept"`)) {
		t.Fatal(wire.String())
	}
	wire.Reset()
	s.handleUserInput(rpcMsg{ID: 43, Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"questions":[{"id":"branch","question":"Which branch?","options":[{"label":"main"},{"label":"test"}]},{"id":"name","question":"Name?"}]}`)})
	if len(requests) != 3 || !bytes.Contains(wire.Bytes(), []byte(`"branch":{"answers":["test"]}`)) || !bytes.Contains(wire.Bytes(), []byte("my answer")) {
		t.Fatal(requests, wire.String())
	}
	if _, err := s.parseJobResult(`{"status":"completed","kind":"action","reply":"test","evidence":"player selected test"}`); err != nil {
		t.Fatalf("completed player selection was rejected: %v", err)
	}
}

func TestUnansweredQuestionsCannotCompleteAction(t *testing.T) {
	s := newAppServerSession("session", "unused", t.TempDir(), "")
	s.stdin = &inquiryResponse{}
	s.SetInquiryHandler(func(_ context.Context, q acp.Inquiry) (string, error) {
		if q.Message == "first" {
			return "choice-1", nil
		}
		return "", context.Canceled
	})
	s.handleUserInput(rpcMsg{ID: 44, Method: "item/tool/requestUserInput", Params: json.RawMessage(`{"questions":[{"id":"a","question":"first","options":[{"label":"yes"}]},{"id":"b","question":"second","options":[{"label":"yes"}]}]}`)})
	if _, err := s.parseJobResult(`{"status":"completed","kind":"action","reply":"done","evidence":"selection"}`); err == nil {
		t.Fatal("partial answers counted as completed tool")
	}
}
