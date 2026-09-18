package store

import (
	"context"
	"time"
)

// UpsertProviderInstallation 持久化本机 Provider 安装状态。
func (s *Store) UpsertProviderInstallation(ctx context.Context, provider, version, path, status string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO provider_installations(provider, version, path, status, updated_at)
VALUES(?, ?, ?, ?, ?)
ON CONFLICT(provider) DO UPDATE SET version=excluded.version, path=excluded.path, status=excluded.status, updated_at=excluded.updated_at
`, provider, version, path, status, now)
	return err
}
