package config

import "testing"

func TestProductionSecurityRequirements(t *testing.T) {
	for k, v := range map[string]string{"AIE_SECRET_MASTER_KEY": "secret-master-test-independent-value-123", "AIE_BACKUP_MASTER_KEY": "backup-master-test-independent-value-456", "AIE_SETUP_TOKEN": "setup-token-test-independent-value-789", "AIE_MASTER_KEY": "", "AIE_DEV_SEED_ADMIN": ""} {
		t.Setenv(k, v)
	}
	cfg := &Config{Env: "production", DatabaseURL: "postgres://test:strong-database-password-123@localhost/test", PublicServerURL: "https://employee.example.com"}
	if err := cfg.validateSecurity(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"AIE_SECRET_MASTER_KEY", "AIE_BACKUP_MASTER_KEY", "AIE_SETUP_TOKEN"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "dev-only")
			if cfg.validateSecurity() == nil {
				t.Fatal("accepted unsafe key")
			}
		})
	}
	t.Run("reuse", func(t *testing.T) {
		t.Setenv("AIE_BACKUP_MASTER_KEY", "secret-master-test-independent-value-123")
		if cfg.validateSecurity() == nil {
			t.Fatal("accepted shared keys")
		}
	})
	t.Run("public HTTP", func(t *testing.T) {
		copy := *cfg
		copy.PublicServerURL = "http://employee.example.com"
		if copy.validateSecurity() == nil {
			t.Fatal("accepted public HTTP")
		}
	})
	t.Run("database", func(t *testing.T) {
		copy := *cfg
		copy.DatabaseURL = "postgres://test:aie@localhost/test"
		if copy.validateSecurity() == nil {
			t.Fatal("accepted weak database password")
		}
	})
}
