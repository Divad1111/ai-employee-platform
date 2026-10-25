// Package statepg persists security state without relying on process-local caches.
package statepg

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type Collection struct {
	DB   *sql.DB
	Kind string
}

func New(db *sql.DB, kind string) *Collection { return &Collection{DB: db, Kind: kind} }
func (c *Collection) Get(ctx context.Context, id string, dst any) error {
	var b []byte
	if err := c.DB.QueryRowContext(ctx, `SELECT payload FROM security_state WHERE kind=$1 AND id=$2`, c.Kind, id).Scan(&b); err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}
func (c *Collection) Put(ctx context.Context, id string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = c.DB.ExecContext(ctx, `INSERT INTO security_state(kind,id,payload) VALUES($1,$2,$3) ON CONFLICT(kind,id) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, c.Kind, id, b)
	return err
}
func (c *Collection) Insert(ctx context.Context, id string, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = c.DB.ExecContext(ctx, `INSERT INTO security_state(kind,id,payload) VALUES($1,$2,$3)`, c.Kind, id, b)
	return err
}
func (c *Collection) Delete(ctx context.Context, id string) error {
	_, err := c.DB.ExecContext(ctx, `DELETE FROM security_state WHERE kind=$1 AND id=$2`, c.Kind, id)
	return err
}
func (c *Collection) All(ctx context.Context) ([][]byte, error) {
	rows, err := c.DB.QueryContext(ctx, `SELECT payload FROM security_state WHERE kind=$1 ORDER BY id`, c.Kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var b []byte
		if err = rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (c *Collection) ResolvePending(ctx context.Context, id string, value any) (bool, error) {
	b, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	result, err := c.DB.ExecContext(ctx, `UPDATE security_state SET payload=$3,updated_at=NOW() WHERE kind=$1 AND id=$2 AND payload->>'status'='PENDING'`, c.Kind, id, b)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

// Claim is atomic across all server processes. Callers must treat errors as a denial.
func Claim(ctx context.Context, db *sql.DB, key string, ttl time.Duration) (bool, error) {
	var accepted string
	err := db.QueryRowContext(ctx, `INSERT INTO security_replay_keys(key,expires_at) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET expires_at=EXCLUDED.expires_at WHERE security_replay_keys.expires_at<=NOW() RETURNING key`, key, time.Now().Add(ttl)).Scan(&accepted)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
