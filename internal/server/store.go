package server

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maxCacheEntries = 10000

type cacheEntry struct {
	key   string
	value FeatureFlag
}

type lruCache struct {
	capacity int
	items    map[string]*list.Element
	order    *list.List
	mu       sync.RWMutex
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		capacity: capacity,
		items:    make(map[string]*list.Element, capacity),
		order:    list.New(),
	}
}

func (c *lruCache) Get(key string) (FeatureFlag, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, exists := c.items[key]; exists {
		c.order.MoveToFront(el)
		return el.Value.(*cacheEntry).value, true
	}
	return FeatureFlag{}, false
}

func (c *lruCache) Add(key string, value FeatureFlag) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, exists := c.items[key]; exists {
		el.Value.(*cacheEntry).value = value
		c.order.MoveToFront(el)
		return
	}

	entry := &cacheEntry{key: key, value: value}
	el := c.order.PushFront(entry)
	c.items[key] = el

	if c.order.Len() > c.capacity {
		c.removeOldest()
	}
}

func (c *lruCache) Remove(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, exists := c.items[key]; exists {
		c.order.Remove(el)
		delete(c.items, key)
	}
}

func (c *lruCache) Entries() []FeatureFlag {
	c.mu.RLock()
	defer c.mu.RUnlock()

	flags := make([]FeatureFlag, 0, c.order.Len())
	for el := c.order.Front(); el != nil; el = el.Next() {
		flags = append(flags, el.Value.(*cacheEntry).value)
	}
	return flags
}

func (c *lruCache) removeOldest() {
	if el := c.order.Back(); el != nil {
		entry := el.Value.(*cacheEntry)
		delete(c.items, entry.key)
		c.order.Remove(el)
	}
}

type FlagStore struct {
	pool  *pgxpool.Pool
	cache *lruCache
}

func NewFlagStore(databaseURL string) (*FlagStore, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	store := &FlagStore{
		pool:  pool,
		cache: newLRUCache(cacheMaxEntries),
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
		s.cache.Add(canonicalName(flag.Name), flag)
	}

	return rows.Err()
}

func canonicalName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (s *FlagStore) List() []FeatureFlag {
	flags := s.cache.Entries()

	sort.Slice(flags, func(i, j int) bool {
		return flags[i].Name < flags[j].Name
	})

	return flags
}

func (s *FlagStore) Get(name string) (FeatureFlag, bool) {
	key := canonicalName(name)

	flag, ok := s.cache.Get(key)
	if ok {
		return flag, true
	}

	flag, err := s.loadFlagFromDB(context.Background(), key)
	if err != nil {
		return FeatureFlag{}, false
	}
	if flag.Name == "" {
		return FeatureFlag{}, false
	}

	s.cache.Add(key, flag)
	return flag, true
}

func (s *FlagStore) loadFlagFromDB(ctx context.Context, name string) (FeatureFlag, error) {
	var flag FeatureFlag
	var rulesJSON []byte

	row := s.pool.QueryRow(ctx, `SELECT name, default_state, rules FROM flags WHERE name = $1`, name)
	if err := row.Scan(&flag.Name, &flag.DefaultState, &rulesJSON); err != nil {
		if err == pgx.ErrNoRows {
			return FeatureFlag{}, nil
		}
		return FeatureFlag{}, fmt.Errorf("failed to query flag: %w", err)
	}

	if err := json.Unmarshal(rulesJSON, &flag.Rules); err != nil {
		return FeatureFlag{}, fmt.Errorf("failed to unmarshall flag rules: %w", err)
	}

	return flag, nil
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

	flag.Name = strings.TrimSpace(flag.Name)
	s.cache.Add(canonicalName(flag.Name), flag)
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

	s.cache.Add(key, flag)
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

	s.cache.Remove(key)
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
