package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"time"
)

// Store permits durable production audit storage with the same query API.
type Store interface {
	Log(context.Context, string, string, string, string, string, map[string]string)
	Query(Filter) []Entry
	ExportJSON(Filter) ([]byte, error)
	Count() int
	ArchiveBefore(time.Time) int
}
type Postgres struct{ db *sql.DB }

func NewPostgres(db *sql.DB) *Postgres { return &Postgres{db: db} }
func (s *Postgres) Log(ctx context.Context, typ, actor, action, result, ip string, meta map[string]string) {
	m := NewMemory()
	m.Log(ctx, typ, actor, action, result, ip, meta)
	e := m.entries[0]
	b, err := json.Marshal(e.Metadata)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `INSERT INTO audit_logs(actor_type,actor_id,action,target_type,target_id,result,ip,metadata,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, e.ActorType, e.ActorID, e.Action, e.TargetType, e.TargetID, e.Result, e.IP, b, e.CreatedAt)
	}
	if err != nil {
		log.Printf("SECURITY AUDIT STORAGE FAILURE: action=%s actor=%s error=%v", action, actor, err)
	}
}
func (s *Postgres) Query(f Filter) []Entry {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	limit := f.Limit
	if limit <= 0 {
		limit = 1000
	}
	if limit > 10000 {
		limit = 10000
	}
	// SQL applies scope filters before LIMIT; keyword filtering follows identical Memory semantics.
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor_type,actor_id,action,target_type,target_id,result,ip,metadata,created_at FROM audit_logs WHERE ($1='' OR lower(actor_id)=lower($1) OR lower(actor_type)=lower($1)) AND ($2='' OR lower(action) LIKE '%'||lower($2)||'%') AND ($3='' OR starts_with(lower(action),lower($3))) AND ($4='' OR lower(target_type)=lower($4)) AND ($5='' OR lower(target_id)=lower($5)) AND ($6='' OR lower(concat_ws(' ',action,actor_id,actor_type,ip,result,target_id,target_type,metadata::text)) LIKE '%'||lower($6)||'%') ORDER BY created_at DESC,id DESC LIMIT $7`, f.Actor, f.Action, f.ActionPrefix, f.TargetType, f.TargetID, f.Keyword, limit)
	if err != nil {
		log.Printf("AUDIT query failed: %v", err)
		return nil
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		var b []byte
		if err = rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.TargetType, &e.TargetID, &e.Result, &e.IP, &b, &e.CreatedAt); err != nil {
			log.Printf("AUDIT scan failed: %v", err)
			return nil
		}
		if json.Unmarshal(b, &e.Metadata) != nil {
			return nil
		}
		out = append(out, e)
	}
	if rows.Err() != nil {
		log.Printf("AUDIT read failed: %v", rows.Err())
		return nil
	}
	return out
}
func (s *Postgres) ExportJSON(f Filter) ([]byte, error) {
	return json.MarshalIndent(map[string]any{"items": s.Query(f), "exported_at": time.Now().UTC()}, "", "  ")
}
func (s *Postgres) Count() int {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM audit_logs`).Scan(&n); err != nil {
		log.Printf("AUDIT count failed: %v", err)
	}
	return n
}
func (s *Postgres) ArchiveBefore(t time.Time) int {
	result, err := s.db.Exec(`DELETE FROM audit_logs WHERE created_at<$1`, t)
	if err != nil {
		log.Printf("AUDIT archive failed: %v", err)
		return 0
	}
	n, _ := result.RowsAffected()
	return int(n)
}
