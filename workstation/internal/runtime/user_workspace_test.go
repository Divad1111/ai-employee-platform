package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedWorkspace(t *testing.T) {
	// Linux may expose a symlinked temporary base; use the real path.
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "projects")
	m := &Managers{workspaces: map[string]*Workspace{}}
	first, err := m.EnsureUserWorkspace("W", "E1", root, "zhangwei", "test")
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureUserWorkspace("W", "E2", root, "zhangwei", "test")
	if err != nil {
		t.Fatal(err)
	}
	if first.Path != filepath.Join(root, "zhangwei", "test") || second.Path != first.Path || m.employeeWorkspace("W", "E1") != first {
		t.Fatal(first, second)
	}
	for _, name := range []string{"../test", "nested/project", `nested\project`, "NUL", "a..b", "x."} {
		if _, err := managedWorkspacePath(root, "zhangwei", name); err == nil {
			t.Fatal(name)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "zhangwei", "link")); err == nil {
		if _, err := managedWorkspacePath(root, "zhangwei", "link"); err == nil || !strings.Contains(err.Error(), "链接") {
			t.Fatal(err)
		}
	}
}
