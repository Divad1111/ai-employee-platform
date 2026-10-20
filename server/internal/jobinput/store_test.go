package jobinput

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestInputFileStorageSurvivesReopen(t *testing.T) {
	root := t.TempDir()
	store := &Store{Root: root}
	file, err := store.Put(`..\路径\需求.pdf`, strings.NewReader("user file"))
	if err != nil || file.Name != "需求.pdf" {
		t.Fatalf("unsafe name or put failed: %+v, %v", file, err)
	}
	reopened := &Store{Root: root}
	r, err := reopened.Open(file.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	data, _ := io.ReadAll(r)
	if !bytes.Equal(data, []byte("user file")) {
		t.Fatal("persisted file unavailable after reopen")
	}
	if _, err := reopened.Open("../outside"); err == nil {
		t.Fatal("hash path traversal accepted")
	}
	if _, err := store.Put("empty.txt", strings.NewReader("")); err == nil {
		t.Fatal("empty file accepted")
	}
}
