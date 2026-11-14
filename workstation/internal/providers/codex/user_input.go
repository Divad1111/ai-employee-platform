package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ai-employee-platform/workstation/internal/acp"
)

// Each answer belongs to its original question ID, even when several questions
// arrive in one request. Ask them in order using the existing Feishu waiter.
func (s *appServerSession) handleUserInput(msg rpcMsg) {
	var request struct {
		Questions []struct {
			ID, Header, Question string
			IsSecret, IsOther    bool
			Options              []struct{ Label, Description string }
		}
	}
	s.mu.Lock()
	handler := s.inquiryHandler
	s.mu.Unlock()
	answers := map[string]any{}
	if handler != nil && json.Unmarshal(msg.Params, &request) == nil {
		for _, q := range request.Questions {
			if q.ID == "" || q.IsSecret {
				break
			}
			inq := acp.Inquiry{SessionID: s.id, Method: msg.Method, Message: strings.TrimSpace(q.Header + "\n" + q.Question), AllowText: q.IsOther || len(q.Options) == 0}
			labels := map[string]string{}
			for i, option := range q.Options {
				id := fmt.Sprintf("choice-%d", i+1)
				labels[id] = option.Label
				inq.Options = append(inq.Options, acp.InquiryOption{OptionID: id, Name: option.Label})
				if option.Description != "" {
					inq.Message += "\n" + option.Label + "：" + option.Description
				}
			}
			id, err := handler(context.Background(), inq)
			if err != nil {
				break
			}
			answer := labels[id]
			if inq.AllowText && strings.HasPrefix(id, "text:") {
				answer = strings.TrimPrefix(id, "text:")
			}
			if answer == "" {
				break
			}
			answers[q.ID] = map[string]any{"answers": []string{answer}}
		}
	}
	// request_user_input is a completed tool operation when every question has
	// an actual player answer, including tasks whose only goal is that choice.
	if len(request.Questions) > 0 && len(answers) == len(request.Questions) {
		s.mu.Lock()
		s.toolCompleted = true
		s.mu.Unlock()
	}
	if err := s.write(map[string]any{"id": msg.ID, "result": map[string]any{"answers": answers}}); err != nil {
		s.failWaiters("无法将玩家选择返回 Codex：" + err.Error())
	}
}
