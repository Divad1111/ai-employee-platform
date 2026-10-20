package taskconversation

import (
	"context"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/feishu"
)

var _ feishu.AttachmentHistory = (*Service)(nil)

// RecentAttachmentJobs scopes persisted manifests to the requesting sender/chat.
// History is informational only and must never refill the pending input buffer.
func (s *Service) RecentAttachmentJobs(ctx context.Context, chatID, ownerOpenID string, limit int) ([]feishu.AttachmentJob, error) {
	if s.Store == nil || s.Jobs == nil || chatID == "" || ownerOpenID == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 5 {
		limit = 5
	}
	ids, err := s.Store.ListOwnerChatIDs(ctx, chatID, ownerOpenID, 100)
	if err != nil {
		return nil, err
	}
	var out []feishu.AttachmentJob
	for _, id := range ids {
		c, err := s.Store.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if c.ChatID != chatID || c.OwnerOpenID != ownerOpenID || c.CurrentJobID == "" {
			continue
		}
		j, err := s.Jobs.Get(ctx, c.CurrentJobID)
		if err != nil {
			return nil, err
		}
		if len(j.InputAttachments) == 0 {
			continue
		}
		out = append(out, feishu.AttachmentJob{JobID: j.ID, EmployeeName: c.EmployeeName, Files: append([]input.File(nil), j.InputAttachments...)})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
