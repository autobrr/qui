// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

// TorznabSearchCacheEntry captures a cached Torznab search response.
type TorznabSearchCacheEntry struct {
	ID                 int64
	CacheKey           string
	Scope              string
	Query              string
	Categories         []int
	IndexerIDs         []int
	RequestFingerprint string
	ResponseData       []byte
	TotalResults       int
	CachedAt           time.Time
	LastUsedAt         time.Time
	ExpiresAt          time.Time
	HitCount           int64
}

// TorznabSearchCacheStats provides aggregated cache metrics for observability.
type TorznabSearchCacheStats struct {
	Entries         int64      `json:"entries"`
	TotalHits       int64      `json:"totalHits"`
	ApproxSizeBytes int64      `json:"approxSizeBytes"`
	OldestCachedAt  *time.Time `json:"oldestCachedAt,omitempty"`
	NewestCachedAt  *time.Time `json:"newestCachedAt,omitempty"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
	Enabled         bool       `json:"enabled"`
	TTLMinutes      int        `json:"ttlMinutes"`
}

// TorznabRecentSearch captures metadata about a cached search request for UI consumption.
type TorznabRecentSearch struct {
	CacheKey     string     `json:"cacheKey"`
	Scope        string     `json:"scope"`
	Query        string     `json:"query"`
	Categories   []int      `json:"categories"`
	IndexerIDs   []int      `json:"indexerIds"`
	TotalResults int        `json:"totalResults"`
	CachedAt     time.Time  `json:"cachedAt"`
	LastUsedAt   *time.Time `json:"lastUsedAt,omitempty"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	HitCount     int64      `json:"hitCount"`
}

// TorznabSearchCacheSettings tracks persisted cache configuration.
type TorznabSearchCacheSettings struct {
	TTLMinutes int
	UpdatedAt  *time.Time
}

// TorznabSearchCacheStore persists search cache entries.
type TorznabSearchCacheStore struct {
	db dbinterface.Querier
}

// NewTorznabSearchCacheStore constructs a new search cache store.
func NewTorznabSearchCacheStore(db dbinterface.Querier) *TorznabSearchCacheStore {
	return &TorznabSearchCacheStore{db: db}
}

// Fetch returns a cached search response by cache key.
func (s *TorznabSearchCacheStore) Fetch(ctx context.Context, cacheKey string) (*TorznabSearchCacheEntry, bool, error) {
	if strings.TrimSpace(cacheKey) == "" {
		return nil, false, fmt.Errorf("cache key cannot be empty")
	}

	const fetchQuery = `
		SELECT id, scope, query, categories_json, indexer_ids_json, request_fingerprint,
		       response_data, total_results, cached_at, last_used_at, expires_at, hit_count
		FROM torznab_search_cache
		WHERE cache_key = ?
	`

	var (
		id             int64
		scope          string
		queryValue     sql.NullString
		categoriesJSON sql.NullString
		indexersJSON   sql.NullString
		fingerprint    string
		response       []byte
		total          int
		cachedAt       time.Time
		lastUsedAt     time.Time
		expiresAt      time.Time
		hitCount       int64
	)

	err := s.db.QueryRowContext(ctx, fetchQuery, cacheKey).Scan(
		&id,
		&scope,
		&queryValue,
		&categoriesJSON,
		&indexersJSON,
		&fingerprint,
		&response,
		&total,
		&cachedAt,
		&lastUsedAt,
		&expiresAt,
		&hitCount,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("fetch torznab search cache: %w", err)
	}

	if time.Now().UTC().After(expiresAt) {
		s.deleteEntry(ctx, id)
		return nil, false, nil
	}

	entry := &TorznabSearchCacheEntry{
		ID:                 id,
		CacheKey:           cacheKey,
		Scope:              scope,
		Query:              strings.TrimSpace(queryValue.String),
		RequestFingerprint: fingerprint,
		ResponseData:       response,
		TotalResults:       total,
		CachedAt:           cachedAt,
		LastUsedAt:         lastUsedAt,
		ExpiresAt:          expiresAt,
		HitCount:           hitCount,
		Categories:         decodeIntArray(categoriesJSON.String),
		IndexerIDs:         decodeIntArray(indexersJSON.String),
	}

	s.touchEntry(ctx, id)

	return entry, true, nil
}

// Store inserts or updates a cached search response.
func (s *TorznabSearchCacheStore) Store(ctx context.Context, entry *TorznabSearchCacheEntry) error {
	if entry == nil {
		return fmt.Errorf("entry cannot be nil")
	}
	if strings.TrimSpace(entry.CacheKey) == "" {
		return fmt.Errorf("cache key cannot be empty")
	}
	if strings.TrimSpace(entry.RequestFingerprint) == "" {
		return fmt.Errorf("request fingerprint cannot be empty")
	}
	if len(entry.ResponseData) == 0 {
		return fmt.Errorf("response data cannot be empty")
	}
	if entry.ExpiresAt.Before(entry.CachedAt) {
		return fmt.Errorf("expiresAt must be after cachedAt")
	}

	categoriesJSON, err := json.Marshal(entry.Categories)
	if err != nil {
		return fmt.Errorf("encode categories: %w", err)
	}
	indexersJSON, err := json.Marshal(entry.IndexerIDs)
	if err != nil {
		return fmt.Errorf("encode indexer ids: %w", err)
	}

	indexerMatcher := buildIndexerMatcher(entry.IndexerIDs)
	const query = `
		INSERT INTO torznab_search_cache (
			cache_key, scope, query, categories_json, indexer_ids_json, indexer_matcher,
			request_fingerprint, response_data, total_results, cached_at, last_used_at, expires_at, hit_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(cache_key) DO UPDATE SET
			scope = excluded.scope,
			query = excluded.query,
			categories_json = excluded.categories_json,
			indexer_ids_json = excluded.indexer_ids_json,
			indexer_matcher = excluded.indexer_matcher,
			request_fingerprint = excluded.request_fingerprint,
			response_data = excluded.response_data,
			total_results = excluded.total_results,
			cached_at = excluded.cached_at,
			last_used_at = excluded.last_used_at,
			expires_at = excluded.expires_at
	`

	if _, err := s.db.ExecContext(
		ctx,
		query,
		entry.CacheKey,
		entry.Scope,
		entry.Query,
		string(categoriesJSON),
		string(indexersJSON),
		indexerMatcher,
		entry.RequestFingerprint,
		entry.ResponseData,
		entry.TotalResults,
		entry.CachedAt,
		entry.LastUsedAt,
		entry.ExpiresAt,
	); err != nil {
		return fmt.Errorf("store torznab search cache entry: %w", err)
	}

	return nil
}

// RecentSearches returns the most recently used cached search queries.
func (s *TorznabSearchCacheStore) RecentSearches(ctx context.Context, scope string, limit int) ([]*TorznabRecentSearch, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}

	scopeFilter := strings.TrimSpace(scope)
	query := `
		SELECT cache_key, scope, COALESCE(query, ''), categories_json, indexer_ids_json,
		       total_results, cached_at, last_used_at, expires_at, hit_count
		FROM torznab_search_cache
		WHERE TRIM(COALESCE(query, '')) != ''
		  AND LOWER(TRIM(COALESCE(query, ''))) != 'test'
	`

	var args []any
	if scopeFilter != "" {
		query += " AND scope = ?"
		args = append(args, scopeFilter)
	}

	query += `
		ORDER BY COALESCE(last_used_at, cached_at) DESC
		LIMIT ?
	`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("recent torznab searches: %w", err)
	}
	defer rows.Close()

	var results []*TorznabRecentSearch
	for rows.Next() {
		var (
			cacheKey       string
			scopeValue     string
			queryValue     string
			categoriesJSON sql.NullString
			indexersJSON   sql.NullString
			totalResults   int
			cachedAt       time.Time
			lastUsed       sql.NullTime
			expiresAt      time.Time
			hitCount       int64
		)

		if err := rows.Scan(
			&cacheKey,
			&scopeValue,
			&queryValue,
			&categoriesJSON,
			&indexersJSON,
			&totalResults,
			&cachedAt,
			&lastUsed,
			&expiresAt,
			&hitCount,
		); err != nil {
			return nil, fmt.Errorf("scan recent torznab searches: %w", err)
		}

		entry := &TorznabRecentSearch{
			CacheKey:     cacheKey,
			Scope:        scopeValue,
			Query:        strings.TrimSpace(queryValue),
			Categories:   decodeIntArray(categoriesJSON.String),
			IndexerIDs:   decodeIntArray(indexersJSON.String),
			TotalResults: totalResults,
			CachedAt:     cachedAt,
			ExpiresAt:    expiresAt,
			HitCount:     hitCount,
		}
		if lastUsed.Valid {
			entry.LastUsedAt = &lastUsed.Time
		}

		results = append(results, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent torznab searches: %w", err)
	}

	return results, nil
}

// FindActiveByScopeAndQuery returns matching, non-expired cache entries for a scope/query pair.
func (s *TorznabSearchCacheStore) FindActiveByScopeAndQuery(ctx context.Context, scope string, query string) ([]*TorznabSearchCacheEntry, error) {
	scope = strings.TrimSpace(scope)
	query = strings.TrimSpace(query)

	if scope == "" || query == "" {
		return nil, nil
	}

	const findQuery = `
		SELECT id, cache_key, scope, query, categories_json, indexer_ids_json, request_fingerprint,
		       response_data, total_results, cached_at, last_used_at, expires_at, hit_count
		FROM torznab_search_cache
		WHERE scope = ? AND query = ? AND expires_at > CURRENT_TIMESTAMP
		ORDER BY LENGTH(indexer_matcher) ASC
	`

	rows, err := s.db.QueryContext(ctx, findQuery, scope, query)
	if err != nil {
		return nil, fmt.Errorf("find torznab search cache entries: %w", err)
	}
	defer rows.Close()

	var entries []*TorznabSearchCacheEntry
	for rows.Next() {
		var (
			id             int64
			cacheKey       string
			scopeValue     string
			queryValue     sql.NullString
			categoriesJSON sql.NullString
			indexersJSON   sql.NullString
			fingerprint    string
			response       []byte
			total          int
			cachedAt       time.Time
			lastUsedAt     time.Time
			expiresAt      time.Time
			hitCount       int64
		)

		if err := rows.Scan(
			&id,
			&cacheKey,
			&scopeValue,
			&queryValue,
			&categoriesJSON,
			&indexersJSON,
			&fingerprint,
			&response,
			&total,
			&cachedAt,
			&lastUsedAt,
			&expiresAt,
			&hitCount,
		); err != nil {
			return nil, fmt.Errorf("scan torznab search cache entries: %w", err)
		}

		entry := &TorznabSearchCacheEntry{
			ID:                 id,
			CacheKey:           cacheKey,
			Scope:              scopeValue,
			Query:              strings.TrimSpace(queryValue.String),
			RequestFingerprint: fingerprint,
			ResponseData:       response,
			TotalResults:       total,
			CachedAt:           cachedAt,
			LastUsedAt:         lastUsedAt,
			ExpiresAt:          expiresAt,
			HitCount:           hitCount,
			Categories:         decodeIntArray(categoriesJSON.String),
			IndexerIDs:         decodeIntArray(indexersJSON.String),
		}

		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate torznab search cache entries: %w", err)
	}

	return entries, nil
}

// CleanupExpired removes all expired cache rows.
func (s *TorznabSearchCacheStore) CleanupExpired(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM torznab_search_cache WHERE expires_at <= CURRENT_TIMESTAMP`)
	if err != nil {
		return 0, fmt.Errorf("cleanup torznab search cache: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("cleanup torznab search cache rows affected: %w", err)
	}
	return deleted, nil
}

// Flush removes every cache entry.
func (s *TorznabSearchCacheStore) Flush(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM torznab_search_cache`)
	if err != nil {
		return 0, fmt.Errorf("flush torznab search cache: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("flush torznab search cache rows affected: %w", err)
	}
	return deleted, nil
}

// InvalidateByIndexerIDs removes cache entries referencing any of the provided indexers.
func (s *TorznabSearchCacheStore) InvalidateByIndexerIDs(ctx context.Context, indexerIDs []int) (int64, error) {
	if len(indexerIDs) == 0 {
		return 0, nil
	}

	var (
		conditions []string
		args       []any
	)
	for _, id := range indexerIDs {
		if id <= 0 {
			continue
		}
		conditions = append(conditions, "indexer_matcher LIKE ?")
		args = append(args, fmt.Sprintf("%%|%d|%%", id))
	}
	if len(conditions) == 0 {
		return 0, nil
	}

	query := fmt.Sprintf("DELETE FROM torznab_search_cache WHERE %s", strings.Join(conditions, " OR "))
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("invalidate torznab search cache: %w", err)
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("invalidate torznab search cache rows affected: %w", err)
	}
	return deleted, nil
}

// Stats returns summary metrics for the search cache table.
func (s *TorznabSearchCacheStore) Stats(ctx context.Context) (*TorznabSearchCacheStats, error) {
	const query = `
		SELECT
			COUNT(*) AS entries,
			COALESCE(SUM(hit_count), 0) AS total_hits,
			COALESCE(SUM(LENGTH(response_data)), 0) AS approx_size,
			MIN(cached_at) AS oldest_cached,
			MAX(cached_at) AS newest_cached,
			MAX(last_used_at) AS last_used
		FROM torznab_search_cache
	`

	var (
		entries      int64
		totalHits    int64
		sizeBytes    int64
		oldestCached sql.NullString
		newestCached sql.NullString
		lastUsed     sql.NullString
	)

	err := s.db.QueryRowContext(ctx, query).Scan(
		&entries,
		&totalHits,
		&sizeBytes,
		&oldestCached,
		&newestCached,
		&lastUsed,
	)
	if err != nil {
		return nil, fmt.Errorf("torznab search cache stats: %w", err)
	}

	stats := &TorznabSearchCacheStats{
		Entries:         entries,
		TotalHits:       totalHits,
		ApproxSizeBytes: sizeBytes,
	}
	if t := parseCacheTimestamp(oldestCached); t != nil {
		stats.OldestCachedAt = t
	}
	if t := parseCacheTimestamp(newestCached); t != nil {
		stats.NewestCachedAt = t
	}
	if t := parseCacheTimestamp(lastUsed); t != nil {
		stats.LastUsedAt = t
	}
	return stats, nil
}

// GetSettings returns the current cache settings (if any).
func (s *TorznabSearchCacheStore) GetSettings(ctx context.Context) (*TorznabSearchCacheSettings, error) {
	const query = `SELECT ttl_minutes, updated_at FROM torznab_search_cache_settings WHERE id = 1`

	var (
		ttlMinutes int
		updatedRaw sql.NullTime
	)

	err := s.db.QueryRowContext(ctx, query).Scan(&ttlMinutes, &updatedRaw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get torznab search cache settings: %w", err)
	}

	settings := &TorznabSearchCacheSettings{
		TTLMinutes: ttlMinutes,
	}
	if updatedRaw.Valid {
		ts := updatedRaw.Time.UTC()
		settings.UpdatedAt = &ts
	}

	return settings, nil
}

// UpdateSettings persists TTL minutes and returns the updated settings.
func (s *TorznabSearchCacheStore) UpdateSettings(ctx context.Context, ttlMinutes int) (*TorznabSearchCacheSettings, error) {
	if ttlMinutes <= 0 {
		return nil, fmt.Errorf("ttlMinutes must be positive")
	}

	const query = `
		INSERT INTO torznab_search_cache_settings (id, ttl_minutes)
		VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET ttl_minutes = excluded.ttl_minutes
	`

	if _, err := s.db.ExecContext(ctx, query, ttlMinutes); err != nil {
		return nil, fmt.Errorf("update torznab search cache settings: %w", err)
	}

	return s.GetSettings(ctx)
}

// RebaseTTL recalculates expires_at for all cached entries using the provided TTL minutes.
func (s *TorznabSearchCacheStore) RebaseTTL(ctx context.Context, ttlMinutes int) (int64, error) {
	if ttlMinutes <= 0 {
		return 0, fmt.Errorf("ttlMinutes must be positive")
	}

	rows, err := s.db.QueryContext(ctx, `SELECT cache_key, cached_at FROM torznab_search_cache`)
	if err != nil {
		return 0, fmt.Errorf("load torznab search cache rows for ttl rebase: %w", err)
	}
	defer rows.Close()

	var (
		totalUpdated int64
		cacheKey     string
		cachedAt     time.Time
		newExpires   time.Time
	)

	for rows.Next() {
		if err := rows.Scan(&cacheKey, &cachedAt); err != nil {
			return 0, fmt.Errorf("scan torznab search cache row for ttl rebase: %w", err)
		}

		newExpires = cachedAt.Add(time.Duration(ttlMinutes) * time.Minute)
		res, err := s.db.ExecContext(ctx, `UPDATE torznab_search_cache SET expires_at = ? WHERE cache_key = ?`, newExpires, cacheKey)
		if err != nil {
			return 0, fmt.Errorf("rebase torznab search cache ttl: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			totalUpdated += n
		}
	}

	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate torznab search cache rows for ttl rebase: %w", err)
	}

	return totalUpdated, nil
}

func (s *TorznabSearchCacheStore) touchEntry(ctx context.Context, id int64) {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC()
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE torznab_search_cache SET last_used_at = ?, hit_count = hit_count + 1 WHERE id = ?`,
		now,
		id,
	); err != nil {
		log.Error().Err(err).Int64("id", id).Msg("torznab search cache touch failed")
	}
}

// Touch updates last_used_at and hit_count for a cache entry.
func (s *TorznabSearchCacheStore) Touch(ctx context.Context, id int64) {
	if id <= 0 {
		return
	}
	s.touchEntry(ctx, id)
}

func (s *TorznabSearchCacheStore) deleteEntry(ctx context.Context, id int64) {
	if ctx == nil {
		ctx = context.Background()
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM torznab_search_cache WHERE id = ?`, id)
}

func decodeIntArray(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var values []int
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		log.Debug().Err(err).Msg("torznab search cache decode int array failed")
		return nil
	}
	return values
}

type cacheTimestampLayout struct {
	layout   string
	location *time.Location
}

var cacheTimestampLayouts = []cacheTimestampLayout{
	{layout: time.RFC3339Nano},
	{layout: time.RFC3339},
	{layout: "2006-01-02 15:04:05.999999999 -0700 MST"},
	{layout: "2006-01-02 15:04:05 -0700 MST"},
	{layout: "2006-01-02 15:04:05.999999999", location: time.UTC},
	{layout: "2006-01-02 15:04:05", location: time.UTC},
}

func parseCacheTimestamp(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	raw := strings.TrimSpace(value.String)
	if raw == "" {
		return nil
	}
	for _, spec := range cacheTimestampLayouts {
		var parsed time.Time
		var err error
		if spec.location != nil {
			parsed, err = time.ParseInLocation(spec.layout, raw, spec.location)
		} else {
			parsed, err = time.Parse(spec.layout, raw)
		}
		if err != nil {
			continue
		}
		t := parsed.UTC()
		return &t
	}
	if unix, err := strconv.ParseFloat(raw, 64); err == nil {
		secs := int64(unix)
		nanos := int64((unix - float64(secs)) * 1_000_000_000)
		t := time.Unix(secs, nanos).UTC()
		return &t
	}
	log.Debug().Str("timestamp", raw).Msg("torznab search cache stats: unrecognized timestamp format")
	return nil
}

func buildIndexerMatcher(ids []int) string {
	if len(ids) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ids)+2)
	parts = append(parts, "")
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	parts = append(parts, "")
	return strings.Join(parts, "|")
}
