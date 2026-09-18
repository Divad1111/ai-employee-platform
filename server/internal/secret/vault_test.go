package secret_test

import (
	"testing"

	"github.com/ai-employee-platform/server/internal/secret"
)

func TestVaultEncryptRoundTripAndRedact(t *testing.T) {
	v, err := secret.NewMemoryVault()
	if err != nil {
		t.Fatal(err)
	}
	ref, err := v.Put("feishu.app_secret", "super-secret-value")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID == "" || ref.Name != "feishu.app_secret" {
		t.Fatal(ref)
	}
	pt, err := v.Get(ref.ID)
	if err != nil || pt != "super-secret-value" {
		t.Fatal(pt, err)
	}
	if secret.Redact(pt) != "***" {
		t.Fatal("Redact")
	}
	list := v.List()
	if len(list) != 1 || list[0].ID != ref.ID {
		t.Fatal(list)
	}
}
