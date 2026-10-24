package infrastructure

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Store 隔离资源存储与任务的原子领取，生产使用 PostgreSQL 行锁。
type Store interface {
	Put(context.Context, string, string, []byte, bool) error
	Get(context.Context, string, string) ([]byte, error)
	Update(context.Context, string, string, func([]byte) ([]byte, error)) error
	List(context.Context, string) ([][]byte, error)
	Delete(context.Context, string, string) error
	Enqueue(context.Context, *Operation) (*Operation, error)
	Claim(context.Context, string) (*Operation, error)
	SaveOperation(context.Context, *Operation) error
	Renew(context.Context, string, string) error
	Operations(context.Context) ([]Operation, error)
	Operation(context.Context, string) (*Operation, error)
	Retry(context.Context, string) (*Operation, error)
	Cancel(context.Context, string) error
}

func save[T any](ctx context.Context, s Store, kind, id string, v T, create bool) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Put(ctx, kind, id, b, create)
}
func load[T any](ctx context.Context, s Store, kind, id string) (v T, err error) {
	b, err := s.Get(ctx, kind, id)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(b, &v)
	return
}
func list[T any](ctx context.Context, s Store, kind string) ([]T, error) {
	rows, err := s.List(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, len(rows))
	for _, b := range rows {
		var v T
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// mutate 在存储锁内只更新需要的字段，后台观测不会覆盖管理员同时修改的配置。
func mutate[T any](ctx context.Context, s Store, kind, id string, fn func(*T) error) (v T, err error) {
	err = s.Update(ctx, kind, id, func(raw []byte) ([]byte, error) {
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		if err := fn(&v); err != nil {
			return nil, err
		}
		return json.Marshal(v)
	})
	return
}

// MemoryStore 仅用于无数据库开发和测试，不共享可变指针。
type MemoryStore struct {
	mu      sync.Mutex
	records map[string][]byte
	ops     map[string]Operation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string][]byte{}, ops: map[string]Operation{}}
}
func (m *MemoryStore) Put(_ context.Context, k, id string, b []byte, create bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := k + "/" + id
	if k == "release" {
		var incoming Release
		if err := json.Unmarshal(b, &incoming); err != nil {
			return err
		}
		for recordKey, raw := range m.records {
			if recordKey == key || !strings.HasPrefix(recordKey, "release/") {
				continue
			}
			var old Release
			if err := json.Unmarshal(raw, &old); err != nil {
				return err
			}
			if old.SoftwareID == incoming.SoftwareID && old.Version == incoming.Version && old.OS == incoming.OS && old.Arch == incoming.Arch {
				return ErrConflict
			}
		}
	}
	if _, ok := m.records[key]; ok && create {
		return ErrConflict
	}
	m.records[key] = append([]byte(nil), b...)
	return nil
}
func (m *MemoryStore) Get(_ context.Context, k, id string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.records[k+"/"+id]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), b...), nil
}

func (m *MemoryStore) Update(_ context.Context, k, id string, fn func([]byte) ([]byte, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.records[k+"/"+id]
	if !ok {
		return ErrNotFound
	}
	b, err := fn(append([]byte(nil), old...))
	if err != nil {
		return err
	}
	m.records[k+"/"+id] = append([]byte(nil), b...)
	return nil
}
func (m *MemoryStore) List(_ context.Context, k string) ([][]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := [][]byte{}
	for key, b := range m.records {
		if len(key) > len(k) && key[:len(k)+1] == k+"/" {
			out = append(out, append([]byte(nil), b...))
		}
	}
	return out, nil
}
func (m *MemoryStore) Delete(_ context.Context, k, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.records, k+"/"+id)
	return nil
}
func cloneOp(o Operation) Operation {
	b, _ := json.Marshal(o.Spec)
	_ = json.Unmarshal(b, &o.Spec)
	return o
}
func active(status string) bool {
	return status == "QUEUED" || status == "RUNNING" || status == "WAITING"
}
func (m *MemoryStore) Enqueue(_ context.Context, o *Operation) (*Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, old := range m.ops {
		if old.Key == o.Key {
			if !reflect.DeepEqual(old.Spec, o.Spec) {
				return nil, ErrConflict
			}
			v := cloneOp(old)
			return &v, nil
		}
	}
	for _, old := range m.ops {
		if old.HostID == o.HostID && active(old.Status) {
			return nil, ErrConflict
		}
	}
	m.ops[o.ID] = cloneOp(*o)
	v := cloneOp(*o)
	return &v, nil
}
func (m *MemoryStore) Claim(_ context.Context, owner string) (*Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	var next *Operation
	for id, o := range m.ops {
		if o.Status == "RUNNING" && o.LeaseUntil.Before(now) {
			o.Status = "INTERRUPTED"
			o.Error = "执行器租约过期，请核实远端状态后重试"
			o.UpdatedAt = now
			m.ops[id] = o
		}
		if (o.Status == "QUEUED" || o.Status == "WAITING") && !o.NotBefore.After(now) && (next == nil || o.CreatedAt.Before(next.CreatedAt)) {
			v := cloneOp(o)
			next = &v
		}
	}
	if next == nil {
		return nil, nil
	}
	next.Status = "RUNNING"
	next.Owner = owner
	next.UpdatedAt = now
	next.LeaseUntil = now.Add(2 * time.Minute)
	m.ops[next.ID] = cloneOp(*next)
	return next, nil
}
func (m *MemoryStore) SaveOperation(_ context.Context, o *Operation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.ops[o.ID]
	if !ok || old.Owner != o.Owner || old.Status != "RUNNING" {
		return ErrConflict
	}
	lease := old.LeaseUntil
	m.ops[o.ID] = cloneOp(*o)
	v := m.ops[o.ID]
	v.LeaseUntil = lease
	m.ops[o.ID] = v
	return nil
}
func (m *MemoryStore) Renew(_ context.Context, id, owner string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.ops[id]
	if !ok || o.Owner != owner || o.Status != "RUNNING" {
		return ErrConflict
	}
	o.LeaseUntil = time.Now().UTC().Add(2 * time.Minute)
	m.ops[id] = o
	return nil
}
func (m *MemoryStore) Operations(_ context.Context) ([]Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Operation{}
	for _, o := range m.ops {
		out = append(out, cloneOp(o))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	limited := []Operation{}
	for i, o := range out {
		if i < 200 || active(o.Status) {
			limited = append(limited, o)
		}
	}
	return limited, nil
}

func (m *MemoryStore) Operation(_ context.Context, id string) (*Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.ops[id]
	if !ok {
		return nil, ErrNotFound
	}
	v := cloneOp(o)
	return &v, nil
}
func (m *MemoryStore) Retry(_ context.Context, id string) (*Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.ops[id]
	if !ok {
		return nil, ErrNotFound
	}
	if o.Status != "FAILED" && o.Status != "INTERRUPTED" {
		return nil, ErrConflict
	}
	for _, other := range m.ops {
		if other.ID != id && other.HostID == o.HostID && active(other.Status) {
			return nil, ErrConflict
		}
	}
	o.Status = "QUEUED"
	o.Error = ""
	o.Owner = ""
	o.NotBefore = time.Now().UTC()
	o.UpdatedAt = o.NotBefore
	m.ops[id] = cloneOp(o)
	return &o, nil
}

func (m *MemoryStore) Cancel(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.ops[id]
	if !ok {
		return ErrNotFound
	}
	if o.Status != "QUEUED" && o.Status != "WAITING" {
		return ErrConflict
	}
	o.Status = "CANCELLED"
	o.Step = "已取消，已创建的远端资源保留"
	o.UpdatedAt = time.Now().UTC()
	m.ops[id] = o
	return nil
}

// PostgresStore 每条资源独立保存；队列使用 SKIP LOCKED 支持多个服务实例。
type PostgresStore struct{ DB *sql.DB }

func NewPostgresStore(db *sql.DB) *PostgresStore { return &PostgresStore{DB: db} }
func dbErr(err error) error {
	var e *pgconn.PgError
	if errors.As(err, &e) && e.Code == "23505" {
		return ErrConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
func (p *PostgresStore) Put(ctx context.Context, k, id string, b []byte, create bool) error {
	q := `INSERT INTO infrastructure_records(kind,id,body) VALUES($1,$2,$3)`
	if !create {
		q += ` ON CONFLICT(kind,id) DO UPDATE SET body=EXCLUDED.body,updated_at=now()`
	}
	_, err := p.DB.ExecContext(ctx, q, k, id, b)
	return dbErr(err)
}
func (p *PostgresStore) Get(ctx context.Context, k, id string) ([]byte, error) {
	var b []byte
	err := p.DB.QueryRowContext(ctx, `SELECT body FROM infrastructure_records WHERE kind=$1 AND id=$2`, k, id).Scan(&b)
	return b, dbErr(err)
}

func (p *PostgresStore) Update(ctx context.Context, k, id string, fn func([]byte) ([]byte, error)) error {
	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT body FROM infrastructure_records WHERE kind=$1 AND id=$2 FOR UPDATE`, k, id).Scan(&raw); err != nil {
		return dbErr(err)
	}
	b, err := fn(raw)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE infrastructure_records SET body=$1,updated_at=now() WHERE kind=$2 AND id=$3`, b, k, id); err != nil {
		return dbErr(err)
	}
	return tx.Commit()
}
func (p *PostgresStore) List(ctx context.Context, k string) ([][]byte, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT body FROM infrastructure_records WHERE kind=$1 ORDER BY id`, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]byte{}
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
func (p *PostgresStore) Delete(ctx context.Context, k, id string) error {
	_, err := p.DB.ExecContext(ctx, `DELETE FROM infrastructure_records WHERE kind=$1 AND id=$2`, k, id)
	return err
}

const opColumns = `id,host_id,actor,idempotency_key,spec,status,step,log,error,owner,created_at,updated_at,not_before,lease_until`

type scanner interface{ Scan(...any) error }

func scanOperation(row scanner) (*Operation, error) {
	var o Operation
	var b []byte
	err := row.Scan(&o.ID, &o.HostID, &o.Actor, &o.Key, &b, &o.Status, &o.Step, &o.Log, &o.Error, &o.Owner, &o.CreatedAt, &o.UpdatedAt, &o.NotBefore, &o.LeaseUntil)
	if err != nil {
		return nil, dbErr(err)
	}
	err = json.Unmarshal(b, &o.Spec)
	return &o, err
}
func (p *PostgresStore) Enqueue(ctx context.Context, o *Operation) (*Operation, error) {
	b, _ := json.Marshal(o.Spec)
	_, err := p.DB.ExecContext(ctx, `INSERT INTO infrastructure_operations(id,host_id,actor,idempotency_key,spec,status,created_at,updated_at,not_before) VALUES($1,$2,$3,$4,$5,$6,$7,$7,$7) ON CONFLICT(idempotency_key) DO NOTHING`, o.ID, o.HostID, o.Actor, o.Key, b, o.Status, o.CreatedAt)
	if err != nil {
		return nil, dbErr(err)
	}
	old, err := scanOperation(p.DB.QueryRowContext(ctx, `SELECT `+opColumns+` FROM infrastructure_operations WHERE idempotency_key=$1`, o.Key))
	if err == nil && !reflect.DeepEqual(old.Spec, o.Spec) {
		return nil, ErrConflict
	}
	return old, err
}
func (p *PostgresStore) Claim(ctx context.Context, owner string) (*Operation, error) {
	_, err := p.DB.ExecContext(ctx, `UPDATE infrastructure_operations SET status='INTERRUPTED',error='执行器租约过期，请核实远端状态后重试',updated_at=now() WHERE status='RUNNING' AND lease_until<now()`)
	if err != nil {
		return nil, err
	}
	o, err := scanOperation(p.DB.QueryRowContext(ctx, `UPDATE infrastructure_operations SET status='RUNNING',owner=$1,updated_at=now(),lease_until=now()+interval '2 minutes' WHERE id=(SELECT id FROM infrastructure_operations WHERE status IN ('QUEUED','WAITING') AND not_before<=now() ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+opColumns, owner))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return o, err
}
func (p *PostgresStore) SaveOperation(ctx context.Context, o *Operation) error {
	res, err := p.DB.ExecContext(ctx, `UPDATE infrastructure_operations SET status=$1,step=$2,log=$3,error=$4,updated_at=now(),not_before=$5 WHERE id=$6 AND owner=$7 AND status='RUNNING'`, o.Status, o.Step, o.Log, o.Error, o.NotBefore, o.ID, o.Owner)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (p *PostgresStore) Renew(ctx context.Context, id, owner string) error {
	res, err := p.DB.ExecContext(ctx, `UPDATE infrastructure_operations SET lease_until=now()+interval '2 minutes' WHERE id=$1 AND owner=$2 AND status='RUNNING'`, id, owner)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (p *PostgresStore) Operations(ctx context.Context) ([]Operation, error) {
	rows, err := p.DB.QueryContext(ctx, `SELECT `+opColumns+` FROM infrastructure_operations WHERE status IN ('QUEUED','RUNNING','WAITING') OR id IN (SELECT id FROM infrastructure_operations ORDER BY created_at DESC LIMIT 200) ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Operation{}
	for rows.Next() {
		o, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *o)
	}
	return out, rows.Err()
}
func (p *PostgresStore) Operation(ctx context.Context, id string) (*Operation, error) {
	return scanOperation(p.DB.QueryRowContext(ctx, `SELECT `+opColumns+` FROM infrastructure_operations WHERE id=$1`, id))
}
func (p *PostgresStore) Retry(ctx context.Context, id string) (*Operation, error) {
	o, err := scanOperation(p.DB.QueryRowContext(ctx, `UPDATE infrastructure_operations SET status='QUEUED',error='',owner='',not_before=now(),updated_at=now() WHERE id=$1 AND status IN ('FAILED','INTERRUPTED') RETURNING `+opColumns, id))
	return o, dbErr(err)
}

func (p *PostgresStore) Cancel(ctx context.Context, id string) error {
	res, err := p.DB.ExecContext(ctx, `UPDATE infrastructure_operations SET status='CANCELLED',step='已取消，已创建的远端资源保留',updated_at=now() WHERE id=$1 AND status IN ('QUEUED','WAITING')`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrConflict
	}
	return nil
}
