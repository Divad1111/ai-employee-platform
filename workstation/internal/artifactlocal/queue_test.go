package artifactlocal_test

import (
	"os"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/artifactlocal"
)

func TestStageKeepsLocalUntilConfirmed(t *testing.T) {
	q := artifactlocal.New(t.TempDir())
	p, err := q.Stage("JOB-1", "out.txt", "txt", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p.LocalPath); err != nil {
		t.Fatal(err)
	}
	if len(q.PendingUploads()) != 1 {
		t.Fatal(q.PendingUploads())
	}
	q.MarkUploaded(p.SHA256)
	if len(q.PendingUploads()) != 0 {
		t.Fatal("应已确认")
	}
	if _, err := os.Stat(p.LocalPath); err != nil {
		t.Fatal("确认后仍应保留本地副本")
	}
}
