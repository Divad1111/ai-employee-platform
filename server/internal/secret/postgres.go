package secret

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ai-employee-platform/server/internal/statepg"
)

type PostgresBindings struct{ data *statepg.Collection }

func NewPostgresBindings(db *sql.DB) *PostgresBindings {
	return &PostgresBindings{statepg.New(db, "secret.bindings")}
}
func (s *PostgresBindings) Save(ctx context.Context, b *Binding) error {
	return s.data.Put(ctx, b.ID, b)
}
func (s *PostgresBindings) Delete(ctx context.Context, id string) error {
	return s.data.Delete(ctx, id)
}
func (s *PostgresBindings) List(ctx context.Context) ([]*Binding, error) {
	data, err := s.data.All(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Binding{}
	for _, b := range data {
		var v Binding
		if err = json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, nil
}
func (s *PostgresBindings) ListByEmployee(ctx context.Context, id string) ([]*Binding, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Binding{}
	for _, v := range all {
		if v.EmployeeID == id {
			out = append(out, v)
		}
	}
	return out, nil
}
func (s *PostgresBindings) ListBySecret(ctx context.Context, id string) ([]*Binding, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Binding{}
	for _, v := range all {
		if v.SecretID == id {
			out = append(out, v)
		}
	}
	return out, nil
}
