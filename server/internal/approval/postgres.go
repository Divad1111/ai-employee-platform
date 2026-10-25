package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ai-employee-platform/server/internal/statepg"
)

type PostgresStore struct{ data *statepg.Collection }

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{statepg.New(db, "approval.requests")}
}
func (s *PostgresStore) Save(ctx context.Context, r *Request) error { return s.data.Put(ctx, r.ID, r) }
func (s *PostgresStore) Get(ctx context.Context, id string) (*Request, error) {
	var r Request
	err := s.data.Get(ctx, id, &r)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
func (s *PostgresStore) List(ctx context.Context) ([]*Request, error) {
	data, err := s.data.All(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Request{}
	for _, b := range data {
		var r Request
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, nil
}
