// Package collector implements the built-in personal work collector.
package collector

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrNotFound = errors.New("记录不存在")
var ErrBusy = errors.New("任务正在执行，请稍后重试")

type Record struct {
	Owner   string          `json:"owner_id"`
	Kind    string          `json:"kind"`
	ID      string          `json:"id"`
	Body    json.RawMessage `json:"body"`
	Updated time.Time       `json:"updated_at"`
}

type Store interface {
	Get(context.Context, string, string, string) (json.RawMessage, error)
	List(context.Context, string, string) ([]Record, error)
	Commit(context.Context, []Record) error
	Delete(context.Context, string, string, string) error
	Take(context.Context, string, string, string) (json.RawMessage, error)
	Acquire(context.Context, string, string, time.Duration) (bool, error)
	Release(context.Context, string, string) error
	Prune(context.Context, time.Time) error
}

func record(owner, kind, id string, value any) Record {
	b, err := json.Marshal(value)
	if err != nil {
		panic("collector: non-JSON record")
	}
	return Record{owner, kind, id, b, time.Now().UTC()}
}

func load[T any](ctx context.Context, st Store, owner, kind, id string) (T, error) {
	var out T
	b, err := st.Get(ctx, owner, kind, id)
	if err == nil {
		err = json.Unmarshal(b, &out)
	}
	return out, err
}

func list[T any](ctx context.Context, st Store, owner, kind string) ([]T, error) {
	rows, err := st.List(ctx, owner, kind)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(rows))
	for _, row := range rows {
		var value T
		if err := json.Unmarshal(row.Body, &value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

type memoryStore struct {
	mu      sync.Mutex
	records map[string]Record
	leases  map[string]lease
}
type lease struct {
	holder  string
	expires time.Time
}

func NewMemoryStore() Store {
	return &memoryStore{records: map[string]Record{}, leases: map[string]lease{}}
}
func key(owner, kind, id string) string { return owner + "\x00" + kind + "\x00" + id }
func (s *memoryStore) Get(_ context.Context, owner, kind, id string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.records[key(owner, kind, id)]
	if !ok {
		return nil, ErrNotFound
	}
	return append(json.RawMessage(nil), r.Body...), nil
}
func (s *memoryStore) List(_ context.Context, owner, kind string) ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Record{}
	for _, r := range s.records {
		if r.Kind == kind && (owner == "*" || r.Owner == owner) {
			r.Body = append(json.RawMessage(nil), r.Body...)
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out, nil
}
func (s *memoryStore) Commit(_ context.Context, rows []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range rows {
		r.Body = append(json.RawMessage(nil), r.Body...)
		s.records[key(r.Owner, r.Kind, r.ID)] = r
	}
	return nil
}
func (s *memoryStore) Delete(_ context.Context, owner, kind, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.records, key(owner, kind, id))
	return nil
}
func (s *memoryStore) Take(_ context.Context, owner, kind, id string) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(owner, kind, id)
	r, ok := s.records[k]
	if !ok {
		return nil, ErrNotFound
	}
	delete(s.records, k)
	return append(json.RawMessage(nil), r.Body...), nil
}
func (s *memoryStore) Acquire(_ context.Context, k, h string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if l, ok := s.leases[k]; ok && l.expires.After(time.Now()) {
		return false, nil
	}
	s.leases[k] = lease{h, time.Now().Add(ttl)}
	return true, nil
}
func (s *memoryStore) Release(_ context.Context, k, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leases[k].holder == h {
		delete(s.leases, k)
	}
	return nil
}

func (s *memoryStore) Prune(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, r := range s.records {
		age := now.Sub(r.Updated)
		remove := (r.Kind == "state" || r.Kind == "confirmation") && age > time.Hour
		if (r.Kind == "event" || r.Kind == "run" || r.Kind == "call") && age > 90*24*time.Hour {
			remove = true
		}
		if r.Kind == "delivery" && age > 90*24*time.Hour {
			var d Delivery
			_ = json.Unmarshal(r.Body, &d)
			remove = d.Status == "sent" || d.Status == "cancelled"
		}
		if remove {
			delete(s.records, k)
		}
	}
	for k, l := range s.leases {
		if now.After(l.expires) {
			delete(s.leases, k)
		}
	}
	return nil
}

type pgStore struct{ db *sql.DB }

func NewPostgresStore(db *sql.DB) Store { return &pgStore{db} }
func (s *pgStore) Get(ctx context.Context, owner, kind, id string) (json.RawMessage, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `SELECT body FROM collector_records WHERE owner_id=$1 AND kind=$2 AND id=$3`, owner, kind, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func (s *pgStore) List(ctx context.Context, owner, kind string) ([]Record, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT owner_id,kind,id,body,updated_at FROM collector_records WHERE kind=$2 AND ($1='*' OR owner_id=$1) ORDER BY updated_at DESC`, owner, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Record{}
	for rows.Next() {
		var r Record
		if err := rows.Scan(&r.Owner, &r.Kind, &r.ID, &r.Body, &r.Updated); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *pgStore) Commit(ctx context.Context, records []Record) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range records {
		_, err = tx.ExecContext(ctx, `INSERT INTO collector_records(owner_id,kind,id,body,updated_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(owner_id,kind,id) DO UPDATE SET body=EXCLUDED.body,updated_at=EXCLUDED.updated_at`, r.Owner, r.Kind, r.ID, []byte(r.Body), r.Updated)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *pgStore) Delete(ctx context.Context, o, k, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM collector_records WHERE owner_id=$1 AND kind=$2 AND id=$3`, o, k, id)
	return err
}
func (s *pgStore) Take(ctx context.Context, o, k, id string) (json.RawMessage, error) {
	var b []byte
	err := s.db.QueryRowContext(ctx, `DELETE FROM collector_records WHERE owner_id=$1 AND kind=$2 AND id=$3 RETURNING body`, o, k, id).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return b, err
}
func (s *pgStore) Acquire(ctx context.Context, k, h string, ttl time.Duration) (bool, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO collector_leases(key,holder,expires_at) VALUES($1,$2,now()+($3 * interval '1 second')) ON CONFLICT(key) DO UPDATE SET holder=EXCLUDED.holder,expires_at=EXCLUDED.expires_at WHERE collector_leases.expires_at <= now()`, k, h, int(ttl.Seconds()))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (s *pgStore) Release(ctx context.Context, k, h string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM collector_leases WHERE key=$1 AND holder=$2`, k, h)
	return err
}

func (s *pgStore) Prune(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM collector_records WHERE
		(kind IN ('state','confirmation') AND updated_at < $1) OR
		(kind IN ('event','run','call') AND updated_at < $2) OR
		(kind='delivery' AND body->>'status' IN ('sent','cancelled') AND updated_at < $2)`,
		now.Add(-time.Hour), now.AddDate(0, 0, -90))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM collector_leases WHERE expires_at < $1`, now)
	return err
}
