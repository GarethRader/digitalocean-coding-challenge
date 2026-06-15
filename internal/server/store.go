package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

type FlagStore struct {
	pool    *pgxpool.Pool
	cache   map[string]FeatureFlag
	cacheMu sync.RWMutex
}

func NewFlagStore(databaseURL string) (*FlagStore, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	store := &FlagStore{
		pool:  pool,
		cache: make(map[string]FeatureFlag),
	}

	if err := store.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	if err := store.loadCache(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

func (s *FlagStore) ensureSchema(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS flags (
			name TEXT PRIMARY KEY,
			default_state BOOLEAN NOT NULL,
			rules JSONB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			user_id TEXT PRIMARY KEY,
			subscription_tier TEXT,
			region TEXT,
			attributes JSONB NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS evaluations (
			id BIGSERIAL PRIMARY KEY,
			flag_name TEXT NOT NULL REFERENCES flags(name) ON DELETE CASCADE,
			enabled BOOLEAN NOT NULL,
			user_id TEXT REFERENCES users(user_id),
			subscription_tier TEXT,
			region TEXT,
			attributes JSONB NOT NULL,
			evaluated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	for _, query := range queries {
		if _, err := s.pool.Exec(ctx, query); err != nil {
			return fmt.Errorf("failed to ensure schema: %w", err)
		}
	}

	return nil
}

func (s *FlagStore) loadCache(ctx context.Context) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	rows, err := s.pool.Query(ctx, `SELECT name, default_state, rules FROM flags`)
	if err != nil {
		return fmt.Errorf("failed to query flags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var flag FeatureFlag
		var rulesJSON []byte
		if err := rows.Scan(&flag.Name, &flag.DefaultState, &rulesJSON); err != nil {
			return fmt.Errorf("failed to scan flag row: %w", err)
		}
		if err := json.Unmarshal(rulesJSON, &flag.Rules); err != nil {
			return fmt.Errorf("failed to unmarshall flag rules: %w", err)
		}
		s.cache[canonicalName(flag.Name)] = flag
	}

	return rows.Err()
}

func canonicalName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (s *FlagStore) List() []FeatureFlag {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()

	flags := make([]FeatureFlag, 0, len(s.cache))
	for _, flag := range s.cache {
		flags = append(flags, flag)
	}

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Name < flags[j].Name
	})

	return flags
}

func (s *FlagStore) Get(name string) (FeatureFlag, bool) {
	s.cacheMu.RLock()
	defer s.cacheMu.RUnlock()
	flag, ok := s.cache[canonicalName(name)]
	return flag, ok
}

func (s *FlagStore) Create(flag FeatureFlag) error {
	key := canonicalName(flag.Name)
	if key == "" {
		return fmt.Errorf("feature flag name is required")
	}

	if _, exists := s.Get(flag.Name); exists {
		return fmt.Errorf("feature flag %q already exists", flag.Name)
	}

	rulesJSON, err := json.Marshal(flag.Rules)
	if err != nil {
		return fmt.Errorf("failed to marshal rules: %w", err)
	}

	ctx := context.Background()
	_, err = s.pool.Exec(ctx, `INSERT INTO flags (name, default_state, rules) VALUES ($1, $2, $3)`, flag.Name, flag.DefaultState, rulesJSON)
	if err != nil {
		return fmt.Errorf("failed to insert flag: %w", err)
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	flag.Name = strings.TrimSpace(flag.Name)
	s.cache[canonicalName(flag.Name)] = flag
	return nil
}

func (s *FlagStore) Update(name string, flag FeatureFlag) error {
	key := canonicalName(name)
	if key == "" {
		return fmt.Errorf("feature flag name is required")
	}

	if _, exists := s.Get(name); !exists {
		return fmt.Errorf("feature flag %q not found", name)
	}

	rulesJSON, err := json.Marshal(flag.Rules)
	if err != nil {
		return fmt.Errorf("failed to marshal rules: %w", err)
	}

	ctx := context.Background()
	_, err = s.pool.Exec(ctx, `UPDATE flags SET default_state = $1, rules = $2 WHERE name = $3`, flag.DefaultState, rulesJSON, name)
	if err != nil {
		return fmt.Errorf("failed to update flag: %w", err)
	}

	flag.Name = name

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.cache[key] = flag
	return nil
}

func (s *FlagStore) Delete(name string) error {
	key := canonicalName(name)
	if key == "" {
		return fmt.Errorf("feature flag name is required")
	}

	if _, exists := s.Get(name); !exists {
		return fmt.Errorf("feature flag %q not found", name)
	}

	ctx := context.Background()
	_, err := s.pool.Exec(ctx, `DELETE FROM flags WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("failed to delete flag: %w", err)
	}

	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	delete(s.cache, key)
	return nil
}

func (s *FlagStore) SaveUser(ctx UserContext) error {
	if strings.TrimSpace(ctx.UserID) == "" {
		return nil
	}

	attributesJSON, err := json.Marshal(ctx.Attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal user attributes: %w", err)
	}

	dbCtx := context.Background()
	_, err = s.pool.Exec(dbCtx, `
		INSERT INTO users (user_id, subscription_tier, region, attributes)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id)
		DO UPDATE SET subscription_tier = EXCLUDED.subscription_tier,
		              region = EXCLUDED.region,
		              attributes = EXCLUDED.attributes
	`, ctx.UserID, ctx.SubscriptionTier, ctx.Region, attributesJSON)
	if err != nil {
		return fmt.Errorf("failed to save user: %w", err)
	}

	return nil
}

func (s *FlagStore) RecordEvaluation(flagName string, userContext UserContext, enabled bool) error {
	attributesJSON, err := json.Marshal(userContext.Attributes)
	if err != nil {
		return fmt.Errorf("failed to marshal evaluation attributes: %w", err)
	}

	userID := sql.NullString{}
	if strings.TrimSpace(userContext.UserID) != "" {
		userID.String = userContext.UserID
		userID.Valid = true
	}

	dbCtx := context.Background()
	_, err = s.pool.Exec(dbCtx, `
		INSERT INTO evaluations (flag_name, enabled, user_id, subscription_tier, region, attributes)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, flagName, enabled, userID, userContext.SubscriptionTier, userContext.Region, attributesJSON)
	if err != nil {
		return fmt.Errorf("failed to record evaluation: %w", err)
	}

	return nil
}

func (s *FlagStore) Close() {
	s.pool.Close()
}
