package runtimeenv

import (
	"strings"
	"testing"
)

func TestBaseDoesNotInheritSecretsOrLoaderOverrides(t *testing.T) {
	t.Setenv("AIE_SECRET_MASTER_KEY", "synthetic-secret")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "synthetic-secret")
	t.Setenv("LD_PRELOAD", "malicious")
	t.Setenv("NODE_OPTIONS", "--require malicious")
	t.Setenv("PATH", "/safe/bin")
	env := strings.Join(Base(), "\n")
	if strings.Contains(env, "synthetic-secret") || strings.Contains(env, "malicious") || !strings.Contains(env, "PATH=/safe/bin") {
		t.Fatal("unsafe child environment")
	}
}
