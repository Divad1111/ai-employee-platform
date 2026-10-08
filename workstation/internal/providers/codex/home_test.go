package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewestCodexHome(t *testing.T) {
	root := t.TempDir()
	older := filepath.Join(root, "a", ".codex")
	newer := filepath.Join(root, "b", ".codex")
	empty := filepath.Join(root, "c", ".codex")
	for _, dir := range []string{older, newer, empty} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeAuth(t, older, time.Now().Add(-time.Hour))
	writeAuth(t, newer, time.Now())
	if got := newestCodexHome([]string{older, newer, empty}); got != newer {
		t.Fatalf("应选择最近登录的目录，得到 %s", got)
	}
}

func writeAuth(t *testing.T, dir string, mod time.Time) {
	t.Helper()
	path := filepath.Join(dir, "auth.json")
	if err := os.WriteFile(path, []byte(`{"tokens":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}
