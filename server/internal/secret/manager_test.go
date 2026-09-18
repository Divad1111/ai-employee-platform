package secret_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ai-employee-platform/server/internal/audit"
	"github.com/ai-employee-platform/server/internal/secret"
)

func TestManagerBindResolveAccessAudit(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	aud := audit.NewMemory()
	mgr := secret.NewManager(v, secret.NewMemoryBindings(), aud)
	ctx := context.Background()
	meta, err := mgr.Put(ctx, "git.token", "ghp_secret_plain", "u1", "", "git")
	if err != nil || meta.Masked != "***" {
		t.Fatal(meta, err)
	}
	b, err := mgr.Bind(ctx, "EMP-1", meta.ID, "GIT_TOKEN", "u1", "")
	if err != nil {
		t.Fatal(err)
	}
	if b.SecretID != meta.ID {
		t.Fatal(b)
	}
	resolved, err := mgr.ResolveForEmployee(ctx, "EMP-1", "SYSTEM", "job", "")
	if err != nil || len(resolved) != 1 || resolved[0].Value != "ghp_secret_plain" {
		t.Fatal(resolved, err)
	}
	if resolved[0].EnvKey != "AIE_GIT_TOKEN" {
		t.Fatal(resolved[0].EnvKey)
	}
	q := aud.Query(audit.Filter{Action: "secret.access", Limit: 10})
	if len(q) < 1 {
		t.Fatal("缺少 secret.access 审计")
	}
	for _, e := range q {
		if e.Metadata["value"] != "" && e.Metadata["value"] != "***" {
			t.Fatal("审计不得含明文")
		}
	}
}

func TestFileVaultRoundTrip(t *testing.T) {
	dir := t.TempDir()
	fv, err := secret.NewFileVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := fv.Put("k", "v1")
	if err != nil {
		t.Fatal(err)
	}
	fv2, err := secret.NewFileVault(dir)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := fv2.Get(ref.ID)
	if err != nil || pt != "v1" {
		t.Fatal(pt, err)
	}
	if err := fv2.Replace(ref.ID, "v2"); err != nil {
		t.Fatal(err)
	}
	pt, _ = fv2.Get(ref.ID)
	if pt != "v2" {
		t.Fatal(pt)
	}
	_ = filepath.Join(dir, "secrets.vault")
}

func TestResolveFailureNoLeak(t *testing.T) {
	v, _ := secret.NewMemoryVault()
	mgr := secret.NewManager(v, secret.NewMemoryBindings(), audit.NewMemory())
	_, err := mgr.Bind(context.Background(), "EMP-1", "missing", "GIT_TOKEN", "u", "")
	if err == nil {
		t.Fatal("应失败")
	}
}
