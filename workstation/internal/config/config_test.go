package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/config"
)

func TestLoadFileForcesMaxSessionsOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(path, []byte("runtime:\n  max_sessions: 2\n"), 0o644)
	cfg, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSessions != 1 {
		t.Fatalf("Q-03 应强制 1, got %d", cfg.MaxSessions)
	}
}
