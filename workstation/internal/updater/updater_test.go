package updater_test

import (
	"errors"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/updater"
)

type okV struct{}

func (okV) Verify([]byte, string, string) error { return nil }

type badV struct{}

func (badV) Verify([]byte, string, string) error { return errors.New("bad") }

func TestInstallRollbackOnHealthFail(t *testing.T) {
	root := t.TempDir()
	fail := false
	m := updater.New(root, okV{}, func() error {
		if fail {
			return errors.New("boom")
		}
		return nil
	})
	m.BeginDrain()
	if err := m.Install("1.0.0", []byte("bin"), "x", "y"); err != nil {
		t.Fatal(err)
	}
	fail = true
	if err := m.Install("1.0.1", []byte("bin2"), "x", "y"); err != updater.ErrHealthFailed {
		t.Fatal(err)
	}
	if m.Snapshot().Current != "1.0.0" {
		t.Fatal(m.Snapshot())
	}
}

func TestRejectBadPackage(t *testing.T) {
	m := updater.New(t.TempDir(), badV{}, nil)
	if err := m.Install("2", []byte("x"), "", ""); err != updater.ErrBadPackage {
		t.Fatal(err)
	}
}
