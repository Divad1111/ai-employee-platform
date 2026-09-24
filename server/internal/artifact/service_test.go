package artifact_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ai-employee-platform/server/internal/artifact"
)

type memJobs map[string]*artifact.JobInfo

func (m memJobs) LookupJob(_ context.Context, id string) (*artifact.JobInfo, error) {
	j, ok := m[id]
	if !ok {
		return nil, artifact.ErrJobRequired
	}
	return j, nil
}

func TestSanitizeName(t *testing.T) {
	ok, err := artifact.SanitizeName("result.json")
	if err != nil || ok != "result.json" {
		t.Fatal(ok, err)
	}
	if _, err := artifact.SanitizeName("../etc/passwd"); err == nil {
		t.Fatal("应拒绝路径穿越")
	}
	if _, err := artifact.SanitizeName("a/b.txt"); err == nil {
		t.Fatal("应拒绝路径分隔")
	}
	if _, err := artifact.SanitizeName(""); err == nil {
		t.Fatal("应拒绝空名")
	}
}

func TestPutSecure_RequiresJobAndRejectsOversized(t *testing.T) {
	svc := artifact.New(artifact.NewMemoryStore(), "")
	svc.SetJobLookup(memJobs{
		"JOB-1": {ID: "JOB-1", WorkstationID: "WS-1", Status: "RUNNING"},
	})

	_, _, err := svc.PutSecure(context.Background(), artifact.PutInput{
		JobID: "JOB-missing", Name: "a.txt", Type: "txt", Body: strings.NewReader("x"),
	})
	if err != artifact.ErrJobRequired {
		t.Fatalf("期望 ErrJobRequired, got %v", err)
	}

	big := bytes.Repeat([]byte("a"), int(artifact.DefaultMaxBytes)+2)
	_, _, err = svc.PutSecure(context.Background(), artifact.PutInput{
		JobID: "JOB-1", Name: "big.bin", Type: "bin", Body: bytes.NewReader(big), MaxBytes: 1024,
	})
	if err != artifact.ErrTooLarge {
		t.Fatalf("期望 ErrTooLarge, got %v", err)
	}
}

func TestPutSecure_WorkstationBind(t *testing.T) {
	svc := artifact.New(artifact.NewMemoryStore(), "")
	svc.SetJobLookup(memJobs{
		"JOB-1": {ID: "JOB-1", WorkstationID: "WS-1", Status: "RUNNING"},
	})
	_, _, err := svc.PutSecure(context.Background(), artifact.PutInput{
		JobID: "JOB-1", Name: "out.json", Type: "json", Body: strings.NewReader(`{}`),
		RequireWSBind: true, WorkstationID: "WS-OTHER", UploadedBy: "workstation",
	})
	if err != artifact.ErrJobForbidden {
		t.Fatalf("期望 ErrJobForbidden, got %v", err)
	}
	a, dedup, err := svc.PutSecure(context.Background(), artifact.PutInput{
		JobID: "JOB-1", Name: "out.json", Type: "json", Body: strings.NewReader(`{}`),
		RequireWSBind: true, WorkstationID: "WS-1", UploadedBy: "workstation",
	})
	if err != nil || dedup || a.WorkstationID != "WS-1" {
		t.Fatal(a, dedup, err)
	}
}

func TestPutDedupByHash(t *testing.T) {
	svc := artifact.New(artifact.NewMemoryStore(), "")
	svc.SetJobLookup(memJobs{"JOB-1": {ID: "JOB-1"}, "JOB-2": {ID: "JOB-2"}})
	body := []byte(`{"ok":true}`)
	a1, d1, err := svc.PutSecure(context.Background(), artifact.PutInput{
		JobID: "JOB-1", Name: "a.json", Type: "json", Body: bytes.NewReader(body),
	})
	if err != nil || d1 {
		t.Fatal(err, d1)
	}
	a2, d2, err := svc.PutSecure(context.Background(), artifact.PutInput{
		JobID: "JOB-2", Name: "b.json", Type: "json", Body: bytes.NewReader(body),
	})
	if err != nil || !d2 {
		t.Fatal(err, d2)
	}
	if a1.SHA256 != a2.SHA256 || a1.Storage != a2.Storage {
		t.Fatal(a1, a2)
	}
}
