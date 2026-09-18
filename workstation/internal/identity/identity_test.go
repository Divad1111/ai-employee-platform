package identity_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/identity"
	"github.com/ai-employee-platform/workstation/internal/platform"
)

func TestGenerateSaveLoadClear(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AIE_DATA_DIR", root)
	paths := platform.Detect()

	b, key, err := identity.Generate()
	if err != nil || key == nil || b.WorkstationID == "" || len(b.KeyPEM) == 0 {
		t.Fatalf("Generate: %v %#v", err, b)
	}
	b.CertPEM = []byte("-----BEGIN CERT-----\nX\n-----END CERT-----\n")
	b.CAPEM = []byte("-----BEGIN CERT-----\nCA\n-----END CERT-----\n")
	if err := identity.Save(paths, b); err != nil {
		t.Fatal(err)
	}
	// 私钥权限应为仅所有者可读（Unix）；Windows 文件 ACL 不映射 Go Mode。
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(paths.IdentityDir(), "client.key"))
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm()&0o077 != 0 {
			t.Fatalf("私钥权限过宽: %v", st.Mode())
		}
	}

	got, err := identity.Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkstationID != b.WorkstationID || string(got.KeyPEM) != string(b.KeyPEM) {
		t.Fatal(got)
	}
	if string(got.CertPEM) == "" || string(got.CAPEM) == "" {
		t.Fatal("证书/CA 未加载")
	}
	if err := identity.Clear(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Load(paths); err == nil {
		t.Fatal("Clear 后应无法 Load")
	}
}
