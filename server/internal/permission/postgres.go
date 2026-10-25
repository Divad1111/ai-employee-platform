package permission

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/ai-employee-platform/server/internal/statepg"
)

type PostgresStore struct{ profiles, rules *statepg.Collection }

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{statepg.New(db, "permission.profiles"), statepg.New(db, "permission.rules")}
}
func (s *PostgresStore) SaveProfile(ctx context.Context, p *Profile) error {
	return s.profiles.Put(ctx, p.ID, p)
}
func (s *PostgresStore) GetProfile(ctx context.Context, id string) (*Profile, error) {
	var p Profile
	err := s.profiles.Get(ctx, id, &p)
	if err == sql.ErrNoRows {
		return nil, ErrProfileNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
func (s *PostgresStore) ListProfiles(ctx context.Context) ([]*Profile, error) {
	data, err := s.profiles.All(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Profile{}
	for _, b := range data {
		var p Profile
		if err = json.Unmarshal(b, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, nil
}
func (s *PostgresStore) DefaultProfileID(ctx context.Context) string {
	ps, err := s.ListProfiles(ctx)
	if err != nil {
		return ""
	}
	for _, p := range ps {
		if p.IsDefault {
			return p.ID
		}
	}
	return ""
}
func (s *PostgresStore) UpsertRule(ctx context.Context, r *Rule) error {
	return s.rules.Put(ctx, r.ProfileID+":"+r.Action, r)
}
func (s *PostgresStore) ListRules(ctx context.Context, profile string) ([]*Rule, error) {
	data, err := s.rules.All(ctx)
	if err != nil {
		return nil, err
	}
	out := []*Rule{}
	for _, b := range data {
		var r Rule
		if err = json.Unmarshal(b, &r); err != nil {
			return nil, err
		}
		if r.ProfileID == profile {
			out = append(out, &r)
		}
	}
	return out, nil
}
func (s *PostgresStore) GetRule(ctx context.Context, profile, action string) (*Rule, error) {
	var r Rule
	err := s.rules.Get(ctx, profile+":"+action, &r)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}
