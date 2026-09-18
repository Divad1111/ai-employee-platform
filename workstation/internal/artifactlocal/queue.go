// Package artifactlocal Workstation 本地产物与待上传队列（断网保留）。
// 设计依据：设计文档 §63、§23。
package artifactlocal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Pending 待上传项。
type Pending struct {
	JobID    string `json:"job_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	SHA256   string `json:"sha256"`
	LocalPath string `json:"local_path"`
	Uploaded bool   `json:"uploaded"`
}

// Queue 本地队列；上传失败不删本地副本。
type Queue struct {
	mu   sync.Mutex
	root string
	items []Pending
}

// New 创建。
func New(root string) *Queue {
	_ = os.MkdirAll(filepath.Join(root, "artifacts"), 0o755)
	return &Queue{root: root}
}

// Stage 写入本地并入队。
func (q *Queue) Stage(jobID, name, typ string, data []byte) (Pending, error) {
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	path := filepath.Join(q.root, "artifacts", sha+"_"+name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Pending{}, err
	}
	p := Pending{JobID: jobID, Name: name, Type: typ, SHA256: sha, LocalPath: path}
	q.mu.Lock()
	q.items = append(q.items, p)
	q.persistLocked()
	q.mu.Unlock()
	return p, nil
}

// MarkUploaded 确认后标记；本地文件仍保留直至显式清理。
func (q *Queue) MarkUploaded(sha string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.items {
		if q.items[i].SHA256 == sha {
			q.items[i].Uploaded = true
		}
	}
	q.persistLocked()
}

// PendingUploads 未确认列表。
func (q *Queue) PendingUploads() []Pending {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []Pending
	for _, p := range q.items {
		if !p.Uploaded {
			out = append(out, p)
		}
	}
	return out
}

func (q *Queue) persistLocked() {
	b, _ := json.Marshal(q.items)
	_ = os.WriteFile(filepath.Join(q.root, "artifacts", "queue.json"), b, 0o600)
}
