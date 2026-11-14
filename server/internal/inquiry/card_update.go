package inquiry

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-employee-platform/server/internal/feishu"
)

func (s *Service) rememberCard(ctx context.Context, id, messageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	inq := s.inquiries[id]
	if inq == nil {
		return nil
	}
	updated := *inq
	updated.CardMessageID = messageID
	if s.Persistent != nil {
		if err := s.Persistent.Put(ctx, id, &updated); err != nil {
			return err
		}
	}
	*inq = updated
	return nil
}

func (s *Service) updateSubmittedCard(inq Inquiry) {
	if s.UpdateCard == nil || inq.CardMessageID == "" || inq.SubmittedCardUpdated {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		name := inq.EmployeeName
		if name == "" && s.employees != nil {
			if emp, err := s.employees.Get(ctx, inq.EmployeeID); err == nil && emp != nil {
				name = emp.Name
			}
		}
		if name == "" {
			name = inq.EmployeeID
		}
		card := feishu.BuildInquirySubmittedCard(inq.JobID, name, inq.SelectedOptionName, inq.ResolvedAt, inq.AllowText)
		// Let the SDK acknowledge the interaction before PATCHing the message.
		// Keep the original schema version; a v2 message cannot become a v1 card.
		var err error
		for _, delay := range []time.Duration{time.Second, 2 * time.Second, 4 * time.Second} {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
			err = s.UpdateCard(ctx, inq.CardMessageID, card.MustJSON())
			if err == nil {
				s.mu.Lock()
				if saved := s.inquiries[inq.ID]; saved != nil {
					next := *saved
					next.SubmittedCardUpdated = true
					if s.Persistent == nil {
						*saved = next
					} else if saveErr := s.Persistent.Put(ctx, next.ID, &next); saveErr == nil {
						*saved = next
					} else {
						fmt.Printf("[Inquiry] 保存卡片状态失败 (inq=%s): %v\n", next.ID, saveErr)
					}
				}
				s.mu.Unlock()
				return
			}
		}
		fmt.Printf("[Inquiry] 更新已提交卡片失败 (inq=%s): %v\n", inq.ID, err)
	}()
}
