// security-migrate re-encrypts existing vault entries, TOTP and backup target configs offline.
// No data changes occur without --apply and --offline-confirmed. Secret values are never printed.
package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-employee-platform/server/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type vaultEntry struct {
	Name string `json:"name"`
	Blob []byte `json:"blob"`
}
type vaultFile struct {
	Entries map[string]vaultEntry `json:"entries"`
}
type rowChange struct{ id, value string }

func gcm(seed string) (cipher.AEAD, error) {
	sum := sha256.Sum256([]byte(seed))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func decrypt(g cipher.AEAD, b []byte) ([]byte, error) {
	ns := g.NonceSize()
	if len(b) < ns {
		return nil, fmt.Errorf("invalid encrypted value")
	}
	return g.Open(nil, b[:ns], b[ns:], nil)
}
func encrypt(g cipher.AEAD, b []byte) ([]byte, error) {
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, b, nil), nil
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "Migration stopped:", err); os.Exit(1) }
func main() {
	if err := run(); err != nil {
		fatal(err)
	}
}
func run() error {
	apply := flag.Bool("apply", false, "apply the validated migration")
	offline := flag.Bool("offline-confirmed", false, "confirm that all control-plane processes are stopped")
	defaults := flag.Bool("from-legacy-defaults", false, "explicitly read the old insecure default keys; never used for new encryption")
	vaultPath := flag.String("vault", filepath.Join(os.Getenv("AIE_SECRET_DIR"), "secrets.vault"), "existing vault file")
	flag.Parse()
	if *apply && !*offline {
		return fmt.Errorf("stop all servers and supply --offline-confirmed before applying")
	}
	nextSecret, nextBackup := os.Getenv("AIE_SECRET_MASTER_KEY"), os.Getenv("AIE_BACKUP_MASTER_KEY")
	if len(nextSecret) < 32 || len(nextBackup) < 32 || strings.Contains(nextSecret, "CHANGE_ME") || strings.Contains(nextBackup, "CHANGE_ME") {
		return fmt.Errorf("configure two new independent random keys (at least 32 characters)")
	}
	oldSecret, oldBackup := os.Getenv("AIE_OLD_SECRET_MASTER_KEY"), os.Getenv("AIE_OLD_BACKUP_MASTER_KEY")
	if *defaults {
		if oldSecret == "" {
			oldSecret = "dev-only-aie-secret-master"
		}
		if oldBackup == "" {
			oldBackup = "ai-employee-platform-default-master-key-v1"
		}
	}
	if oldSecret == "" || oldBackup == "" {
		return fmt.Errorf("explicit old keys are required")
	}
	if nextSecret == oldSecret || nextBackup == oldBackup {
		return fmt.Errorf("new keys must differ from previous keys")
	}
	oldS, err := gcm(oldSecret)
	if err != nil {
		return err
	}
	newS, err := gcm(nextSecret)
	if err != nil {
		return err
	}
	oldB, err := gcm(oldBackup)
	if err != nil {
		return err
	}
	newB, err := gcm(nextBackup)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	db, err := sql.Open("pgx", config.LoadDatabaseDSN())
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(719428632)`); err != nil {
		return err
	}
	// Validate all encrypted values before preparing any replacement.
	var original, updated []byte
	var entries int
	original, err = os.ReadFile(*vaultPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		var data vaultFile
		if err = json.Unmarshal(original, &data); err != nil {
			return err
		}
		for id, entry := range data.Entries {
			plain, err := decrypt(oldS, entry.Blob)
			if err != nil {
				return fmt.Errorf("vault entry %s cannot be decrypted with old key", id)
			}
			entry.Blob, err = encrypt(newS, plain)
			if err != nil {
				return err
			}
			data.Entries[id] = entry
			entries++
		}
		updated, err = json.MarshalIndent(data, "", "  ")
		if err != nil {
			return err
		}
	}
	totpChanges, err := convertRows(ctx, tx, `SELECT user_id::text,secret_ref FROM user_totp FOR UPDATE`, func(value string) (string, error) {
		if !strings.HasPrefix(value, "enc:v1:") {
			return value, nil
		}
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "enc:v1:"))
		if err != nil {
			return "", err
		}
		plain, err := decrypt(oldS, raw)
		if err != nil {
			return "", err
		}
		next, err := encrypt(newS, plain)
		if err != nil {
			return "", err
		}
		return "enc:v1:" + base64.RawURLEncoding.EncodeToString(next), nil
	})
	if err != nil {
		return fmt.Errorf("TOTP migration validation: %w", err)
	}
	backupChanges, err := convertRows(ctx, tx, `SELECT id::text,config_encrypted FROM backup_destinations FOR UPDATE`, func(value string) (string, error) {
		if json.Valid([]byte(value)) && strings.HasPrefix(strings.TrimSpace(value), "{") {
			next, err := encrypt(newB, []byte(value))
			if err != nil {
				return "", err
			}
			return base64.StdEncoding.EncodeToString(next), nil
		}
		raw, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return "", err
		}
		plain, err := decrypt(oldB, raw)
		if err != nil {
			return "", err
		}
		next, err := encrypt(newB, plain)
		if err != nil {
			return "", err
		}
		return base64.StdEncoding.EncodeToString(next), nil
	})
	if err != nil {
		return fmt.Errorf("backup config migration validation: %w", err)
	}
	fmt.Printf("Validated: vault=%d TOTP=%d backup_targets=%d; apply=%t\n", entries, len(totpChanges), len(backupChanges), *apply)
	if !*apply {
		return nil
	}
	for _, change := range totpChanges {
		if _, err = tx.ExecContext(ctx, `UPDATE user_totp SET secret_ref=$2 WHERE user_id=$1::uuid`, change.id, change.value); err != nil {
			return err
		}
	}
	for _, change := range backupChanges {
		if _, err = tx.ExecContext(ctx, `UPDATE backup_destinations SET config_encrypted=$2 WHERE id::text=$1`, change.id, change.value); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM admin_web_sessions`); err != nil {
		return err
	}
	// The old encrypted vault is retained with restrictive permissions for recovery.
	var recoveryPath string
	if updated != nil {
		recoveryPath = *vaultPath + ".before-key-migration-" + time.Now().UTC().Format("20060102T150405.000000000Z")
		recovery, err := os.OpenFile(recoveryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := recovery.Write(original)
		syncErr := recovery.Sync()
		closeErr := recovery.Close()
		if writeErr != nil {
			return writeErr
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err = atomicWrite(*vaultPath, updated); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		// Commit errors can be ambiguous. Do not silently guess which DB key is live.
		return fmt.Errorf("database commit failed; keep server stopped and reconcile vault using %s: %w", recoveryPath, err)
	}
	fmt.Println("Migration complete. Retain old backup key offline for historical backup recovery. Existing backup artifacts were not rewritten.")
	return nil
}
func convertRows(ctx context.Context, tx *sql.Tx, query string, convert func(string) (string, error)) ([]rowChange, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []rowChange{}
	for rows.Next() {
		var id, value string
		if err = rows.Scan(&id, &value); err != nil {
			return nil, err
		}
		if value == "" {
			continue
		}
		next, err := convert(value)
		if err != nil {
			return nil, fmt.Errorf("record %s: %w", id, err)
		}
		if next != value {
			out = append(out, rowChange{id, next})
		}
	}
	return out, rows.Err()
}
func atomicWrite(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".key-migration-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}
