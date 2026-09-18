package artifact_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/artifact"
)

func TestPutDedupByHash(t *testing.T) {
	svc := artifact.New(artifact.NewMemoryStore(), "")
	ctx := context.Background()
	a1, dup, err := svc.Put(ctx, "JOB-1", "result.json", "json", bytes.NewReader([]byte(`{"ok":1}`)))
	if err != nil || dup {
		t.Fatal(err, dup)
	}
	a2, dup, err := svc.Put(ctx, "JOB-2", "result-copy.json", "json", bytes.NewReader([]byte(`{"ok":1}`)))
	if err != nil || !dup {
		t.Fatal(err, dup)
	}
	if a1.SHA256 != a2.SHA256 || a1.Storage != a2.Storage {
		t.Fatal(a1, a2)
	}
	rc, meta, err := svc.Open(ctx, a2.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if meta.Name != "result-copy.json" {
		t.Fatal(meta)
	}
}
