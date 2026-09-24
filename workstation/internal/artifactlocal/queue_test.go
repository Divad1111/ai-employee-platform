package artifactlocal_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/artifactlocal"
)

func TestStagePendingMarkUploadedAndReload(t *testing.T) {
	dir := t.TempDir()
	q := artifactlocal.New(dir)
	p, err := q.Stage("JOB-1", "out.txt", "txt", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.PendingUploads()) != 1 {
		t.Fatal(q.PendingUploads())
	}
	if _, err := os.Stat(p.LocalPath); err != nil {
		t.Fatal(err)
	}
	q.MarkUploaded(p.SHA256)
	if len(q.PendingUploads()) != 0 {
		t.Fatal(q.PendingUploads())
	}
	// 重启后从 queue.json 恢复
	q2 := artifactlocal.New(dir)
	if len(q2.PendingUploads()) != 0 {
		t.Fatal("已上传项不应再 pending", q2.PendingUploads())
	}
	if _, err := artifactlocal.SanitizeName("../x"); err == nil {
		t.Fatal("应拒绝危险文件名")
	}
	_ = filepath.Base(p.LocalPath)
}
