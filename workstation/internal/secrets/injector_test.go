package secrets_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/ai-employee-platform/workstation/internal/secrets"
)

func TestBuildEnvAndSanitize(t *testing.T) {
	env, err := secrets.BuildEnv([]string{"PATH=/bin"}, []secrets.Ref{
		{EnvKey: "AIE_GIT_TOKEN", SecretID: "sec-1", Value: "tok_secret_abc"},
	})
	if err != nil || len(env) != 2 {
		t.Fatal(err, env)
	}
	err = secrets.SanitizeError(errors.New("failed tok_secret_abc"), []string{"tok_secret_abc"})
	if err == nil || strings.Contains(err.Error(), "tok_secret_abc") {
		t.Fatal(err)
	}
	d := secrets.DescribeRefs([]secrets.Ref{{EnvKey: "AIE_GIT_TOKEN", SecretID: "sec-1", Value: "x"}})
	if strings.Contains(strings.Join(d, ""), "x") {
		t.Fatal(d)
	}
}
