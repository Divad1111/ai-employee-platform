package runtime_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/platform"
	"github.com/ai-employee-platform/workstation/internal/providers"
	local "github.com/ai-employee-platform/workstation/internal/runtime"
)

func TestWorkspaceIsolation(t *testing.T) {
	t.Setenv("AIE_DATA_DIR", t.TempDir())
	paths := platform.Detect()
	m := local.NewManagers(paths, providers.NewRegistry())
	allowed := filepath.Join(paths.WorkspaceRoot(), "EMP-1", "project")
	for _, path := range []string{"/", t.TempDir(), filepath.Join(paths.WorkspaceRoot(), "EMP-2", "project"), "relative"} {
		if _, err := m.EnsureWorkspace("W", "EMP-1", path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	if _, err := m.EnsureEmployee("../escape", "bad", "cursor"); err == nil {
		t.Fatal("ID traversal accepted")
	}
	if _, err := m.EnsureWorkspace("W", "EMP-1", allowed); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(allowed, "link")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skip("symlinks unavailable", err)
	}
	if _, err := m.EnsureWorkspace("W2", "EMP-1", filepath.Join(link, "escape")); err == nil {
		t.Fatal("symlink accepted")
	}
}
