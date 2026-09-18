package providers_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/providers"
)

func TestInstallVerifyAndRejectTamper(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	inst, err := providers.NewInstaller(t.TempDir(), pubB64)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("agent-bin")
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sum[:]))
	if err := inst.Install("cursor", "1.0.0", content, sha, sig); err != nil {
		t.Fatal(err)
	}
	if inst.Status("cursor") != providers.Installed {
		t.Fatal(inst.Status("cursor"))
	}
	if len(inst.Events()) == 0 || inst.Events()[0] != "PROVIDER_INSTALLED" {
		t.Fatal(inst.Events())
	}
	bad := append([]byte{}, content...)
	bad[0] ^= 1
	if err := inst.Install("cursor", "1.0.1", bad, sha, sig); err != providers.ErrChecksum {
		t.Fatal(err)
	}
}
