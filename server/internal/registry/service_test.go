package registry_test

import (
	"context"
	"testing"

	"github.com/ai-employee-platform/server/internal/registry"
)

func TestVerifyPackageAndRejectTamper(t *testing.T) {
	svc, priv, err := registry.New(registry.NewMemoryStore(), "")
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("provider-binary-v1")
	sha, sig := registry.SignContent(priv, content)
	if err := svc.VerifyPackage(content, sha, sig); err != nil {
		t.Fatal(err)
	}
	tampered := append([]byte{}, content...)
	tampered[0] ^= 0xff
	if err := svc.VerifyPackage(tampered, sha, sig); err != registry.ErrBadChecksum && err != registry.ErrBadSignature {
		// SHA mismatch first
		if err != registry.ErrBadChecksum {
			t.Fatal(err)
		}
	}
	badSig := sig[:len(sig)-2] + "aa"
	if err := svc.VerifyPackage(content, sha, badSig); err != registry.ErrBadSignature {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = svc.UpsertProvider(ctx, &registry.Provider{ID: "cursor", Name: "Cursor", Capabilities: map[string]bool{"acp": true, "headless": true}})
	v, err := svc.AddVersion(ctx, &registry.Version{
		ProviderID: "cursor", Version: "1.0.0", OS: "windows", Arch: "amd64",
		DownloadURL: "https://example/cursor.zip", SHA256: sha, Signature: sig,
	})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := svc.QueryVersions(ctx, "cursor", "windows", "amd64")
	if len(list) != 1 || list[0].ID != v.ID {
		t.Fatal(list)
	}
	if svc.SigningPublicKey() == "" {
		t.Fatal("公钥应可下发")
	}
}
