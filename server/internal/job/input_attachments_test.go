package job_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	input "github.com/ai-employee-platform/gen/jobinput"
	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/eventbus"
	"github.com/ai-employee-platform/server/internal/job"
)

func TestInputAttachmentsAreImmutableAndIncludedInIdempotency(t *testing.T) {
	ctx := context.Background()
	s := job.NewService(job.NewMemoryStore(), audit.NewMemory(), eventbus.New(10))
	f := input.File{SHA256: strings.Repeat("a", 64), Name: "a.txt", Size: 1}
	in := job.CreateInput{EmployeeID: "EMP", IdempotencyKey: "with-file", InputAttachments: []input.File{f}}
	j, _, err := s.Create(ctx, in, "u", "")
	if err != nil {
		t.Fatal(err)
	}
	j.InputAttachments[0].Name = "changed.txt"
	j, _ = s.Get(ctx, j.ID)
	if j.InputAttachments[0] != f {
		t.Fatal("manifest modified through returned object")
	}
	in.InputAttachments[0].SHA256 = strings.Repeat("b", 64)
	if _, _, err := s.Create(ctx, in, "u", ""); !errors.Is(err, job.ErrIdempotencyConflict) {
		t.Fatal("different attachment reused idempotent task")
	}
	var untrusted job.CreateInput
	if err := json.Unmarshal([]byte(`{"employee_id":"EMP","input_attachments":[{"sha256":"forged"}]}`), &untrusted); err != nil {
		t.Fatal(err)
	}
	if len(untrusted.InputAttachments) != 0 {
		t.Fatal("public JSON could forge an attachment manifest")
	}
}
