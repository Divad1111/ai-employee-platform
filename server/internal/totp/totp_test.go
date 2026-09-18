package totp_test

import (
	"testing"
	"time"

	"github.com/ai-employee-platform/server/internal/totp"
)

func TestGenerateAndVerify(t *testing.T) {
	sec, err := totp.GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := totp.CodeAt(sec, now)
	if err != nil {
		t.Fatal(err)
	}
	if !totp.Verify(sec, code, now) {
		t.Fatal("应校验通过")
	}
	if totp.Verify(sec, "000000", now) {
		t.Fatal("错误码不应通过")
	}
}
