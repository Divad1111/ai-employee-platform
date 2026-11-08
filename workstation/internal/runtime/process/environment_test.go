package process_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-employee-platform/workstation/internal/runtime/process"
)

func TestOSRunnerEnvironmentHelper(t *testing.T) {
	if len(os.Args) < 3 || os.Args[len(os.Args)-2] != "aie-process-env-helper" {
		return
	}
	if os.Getenv("AIE_PARENT_SECRET") != "" {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte(os.Getenv("EXPLICIT_JOB_ENV")), 0o600); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestOSRunnerFiltersInheritedEnvironment(t *testing.T) {
	t.Setenv("AIE_PARENT_SECRET", "must-not-leak")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		output := filepath.Join(t.TempDir(), "child-env")
		spec := process.Spec{Binary: binary, Args: []string{"-test.run=^TestOSRunnerEnvironmentHelper$", "--", "aie-process-env-helper", output}}
		if explicit {
			spec.Env = []string{"EXPLICIT_JOB_ENV=allowed"}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, wait, _, err := (process.OSRunner{}).Start(ctx, spec)
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		err = wait()
		cancel()
		if err != nil {
			t.Fatal("unsafe environment inherited", err)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if explicit && string(data) != "allowed" {
			t.Fatal("explicit job environment lost")
		}
	}
}
