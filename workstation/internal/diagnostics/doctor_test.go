package diagnostics_test

import (
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/diagnostics"
)

func TestDoctorFixWhitelist(t *testing.T) {
	root := t.TempDir()
	r := &diagnostics.Runner{DataDir: root}
	rep := r.Run()
	if len(rep.Checks) < 5 {
		t.Fatal(rep)
	}
	fixed, _, err := r.Fix(true)
	if err != nil {
		t.Fatal(err)
	}
	_ = fixed
	_, _, err = r.Fix(false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := filepath.Abs(filepath.Join(root, "logs")); err != nil {
		t.Fatal(err)
	}
	if !diagnostics.FixWhitelist["logs_dir"] {
		t.Fatal("白名单")
	}
}
