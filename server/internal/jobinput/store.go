package jobinput

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	input "github.com/ai-employee-platform/gen/jobinput"
)

// Store persists content by hash; per-job access is enforced by the API using
// the immutable attachment manifest saved atomically with the Job.
type Store struct{ Root string }

func (s *Store) Put(name string, body io.Reader) (input.File, error) {
	var result input.File
	data, err := io.ReadAll(io.LimitReader(body, input.MaxBytes+1))
	if err != nil {
		return result, err
	}
	if len(data) == 0 || int64(len(data)) > input.MaxBytes {
		return result, fmt.Errorf("附件为空或超过 32 MiB 限制")
	}
	sum := sha256.Sum256(data)
	result = input.File{SHA256: hex.EncodeToString(sum[:]), Name: input.SafeName(name), MIMEType: http.DetectContentType(data), Size: int64(len(data))}
	if err := os.MkdirAll(s.Root, 0o750); err != nil {
		return input.File{}, err
	}
	f, err := os.CreateTemp(s.Root, ".incoming-*")
	if err != nil {
		return input.File{}, err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return input.File{}, err
	}
	if err = f.Close(); err != nil {
		return input.File{}, err
	}
	path := filepath.Join(s.Root, result.SHA256)
	if _, err := os.Stat(path); err == nil {
		return result, nil
	}
	if err = os.Rename(f.Name(), path); err != nil {
		if _, exists := os.Stat(path); exists != nil {
			return input.File{}, err
		}
	}
	return result, nil
}

func (s *Store) Open(hash string) (*os.File, error) {
	if !input.ValidHash(hash) {
		return nil, os.ErrNotExist
	}
	return os.Open(filepath.Join(s.Root, hash))
}
