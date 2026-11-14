package inquiry_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	aiev1 "github.com/ai-employee-platform/gen/go/aie/v1"
	"github.com/ai-employee-platform/server/internal/feishu"
	"github.com/ai-employee-platform/server/internal/inquiry"
	"github.com/ai-employee-platform/server/internal/job"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

type retryCommandQueue struct {
	mockCommandPusher
	unavailable bool
}

func TestCardInputSubmission(t *testing.T) {
	for _, tc := range []struct {
		name, actor, answer string
		allowText, accepted bool
	}{
		{"multiline", "player", "第一行  两个空格\n第二行 <tag>", true, true},
		{"empty", "player", " \n ", true, false},
		{"wrong owner", "someone-else", "text", true, false},
		{"permission prompt", "player", "text", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			j := &job.Job{ID: "job", EmployeeID: "emp", WorkstationID: "ws", Status: job.StatusRunning}
			jobs := &mockJobManager{jobs: map[string]*job.Job{"job": j}}
			queue := &mockCommandPusher{}
			s := inquiry.NewService(queue, jobs, nil, nil, &mockChatResolver{chats: map[string]string{"job": "chat"}})
			s.ApproverForJob = func(context.Context, string) string { return "player" }
			updates := make(chan string, 1)
			s.UpdateCard = func(_ context.Context, id, body string) error {
				if id != "original-card" {
					t.Error(id)
				}
				updates <- body
				return nil
			}
			request, _ := json.Marshal(map[string]any{"inquiry_id": "question", "allow_text": tc.allowText, "message": "Name?"})
			s.HandleInquiryEvent(ctx, "ws", &aiev1.Event{JobId: "job", EmployeeId: "emp", PayloadJson: string(request)})
			raw, _ := json.Marshal(map[string]any{"event": map[string]any{"context": map[string]string{"open_message_id": "original-card"}, "operator": map[string]string{"open_id": tc.actor}, "action": map[string]any{"tag": "button", "name": feishu.InquiryTextSubmitPrefix + "question", "form_value": map[string]string{feishu.InquiryAnswerField: tc.answer}}}})
			var event callback.CardActionTriggerEvent
			if err := json.Unmarshal(raw, &event); err != nil {
				t.Fatal(err)
			}
			response, err := s.HandleCardAction(ctx, &event)
			if err != nil || (response.Toast.Type == "success") != tc.accepted {
				t.Fatal(response, err)
			}
			if tc.accepted {
				if len(queue.commands) != 1 || j.Status != job.StatusRunning || s.GetInquiry("question").ResolvedVia != "飞书卡片输入框" {
					t.Fatal("input not resumed")
				}
				var payload map[string]string
				json.Unmarshal([]byte(queue.commands[0].PayloadJson), &payload)
				if payload["selected_option_id"] != "text:"+tc.answer || response.Card != nil {
					t.Fatal(payload, response)
				}
				s.HandleCardAction(ctx, &event)
				if len(queue.commands) != 1 {
					t.Fatal("duplicate submit resumed twice")
				}
				select {
				case body := <-updates:
					if !strings.Contains(body, "已提交") || !strings.Contains(body, `"schema":"2.0"`) || strings.Contains(body, `"tag":"input"`) {
						t.Fatal(body)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("original card never updated")
				}
			} else if len(queue.commands) != 0 || j.Status != job.StatusWaitingApproval {
				t.Fatal("invalid input resumed task")
			}
		})
	}
}

func (q *retryCommandQueue) PushCommand(ws string, typ aiev1.CommandType, employee, id, payload string) (*aiev1.Command, error) {
	if q.unavailable {
		return nil, errors.New("queue unavailable")
	}
	return q.mockCommandPusher.PushCommand(ws, typ, employee, id, payload)
}

type transientSender struct {
	attempts chan feishu.Reply
	first    bool
}

func (s *transientSender) Send(_ context.Context, r feishu.Reply) error {
	s.attempts <- r
	if s.first {
		s.first = false
		return errors.New("rate limited")
	}
	return nil
}

func TestPendingInquiryRetriesSameCard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	j := &job.Job{ID: "j", EmployeeID: "e", WorkstationID: "w", Status: job.StatusRunning}
	jobs := &mockJobManager{jobs: map[string]*job.Job{"j": j}}
	fs := feishu.NewService(nil)
	sender := &transientSender{attempts: make(chan feishu.Reply, 2), first: true}
	fs.Sender = sender
	s := inquiry.NewService(&mockCommandPusher{}, jobs, nil, fs, &mockChatResolver{chats: map[string]string{"j": "chat"}})
	s.ApproverForJob = func(context.Context, string) string { return "player" }
	s.HandleInquiryEvent(ctx, "w", &aiev1.Event{JobId: "j", EmployeeId: "e", PayloadJson: `{"inquiry_id":"decision","message":"Confirm?"}`})
	first := <-sender.attempts
	select {
	case retry := <-sender.attempts:
		if retry.Content != first.Content || retry.ChatID != "chat" || s.GetInquiry("decision").Status != "PENDING" {
			t.Fatal("retry changed pending decision")
		}
	case <-ctx.Done():
		t.Fatal("card not retried")
	}
}

func TestPlayerAnswerCanRetryAfterQueueFailure(t *testing.T) {
	ctx := context.Background()
	j := &job.Job{ID: "job", EmployeeID: "emp", WorkstationID: "ws", Status: job.StatusRunning}
	jobs := &mockJobManager{jobs: map[string]*job.Job{"job": j}}
	queue := &retryCommandQueue{unavailable: true}
	s := inquiry.NewService(queue, jobs, nil, nil, &mockChatResolver{chats: map[string]string{"job": "chat"}})
	s.ApproverForJob = func(context.Context, string) string { return "player" }
	s.HandleInquiryEvent(ctx, "ws", &aiev1.Event{JobId: "job", EmployeeId: "emp", PayloadJson: `{"inquiry_id":"question","allow_text":true,"message":"Name?"}`})
	for _, actor := range []string{"someone-else", "player"} {
		if _, err := s.CheckChatReply(ctx, "chat", "question release 17", actor); err == nil {
			t.Fatal("unexpected acceptance")
		}
		if j.Status != job.StatusWaitingApproval || s.GetInquiry("question").Status != "PENDING" {
			t.Fatal("answer lost")
		}
	}
	queue.unavailable = false
	handled, err := s.CheckChatReply(ctx, "chat", "question release 17", "player")
	if err != nil || !handled || j.Status != job.StatusRunning || len(queue.commands) != 1 || s.GetInquiry("question").SelectedOptionID != "text:release 17" {
		t.Fatal(handled, err, j.Status, queue.commands)
	}
}
