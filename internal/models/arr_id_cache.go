// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// ExternalIDs represents the external IDs resolved from ARR instances
type ExternalIDs struct {
	IMDbID   string `json:"imdb_id,omitempty"`
	TMDbID   int    `json:"tmdb_id,omitempty"`
	TVDbID   int    `json:"tvdb_id,omitempty"`
	TVMazeID int    `json:"tvmaze_id,omitempty"`
}

// IsEmpty returns true if no IDs are set
func (e *ExternalIDs) IsEmpty() bool {
	return e.IMDbID == "" && e.TMDbID == 0 && e.TVDbID == 0 && e.TVMazeID == 0
}

// ArrIDCacheEntry represents a cached ID lookup result
type ArrIDCacheEntry struct {
	ID            int64       `json:"id"`
	TitleHash     string      `json:"title_hash"`
	ContentType   string      `json:"content_type"`
	ArrInstanceID *int        `json:"arr_instance_id,omitempty"`
	ExternalIDs   ExternalIDs `json:"external_ids"`
	Titles        []string    `json:"titles,omitempty"`
	HasTitles     bool        `json:"has_titles"`
	IsNegative    bool        `json:"is_negative"`
	CachedAt      time.Time   `json:"cached_at"`
	ExpiresAt     time.Time   `json:"expires_at"`
}

// ArrIDCacheStore manages the ARR ID cache in the database
type ArrIDCacheStore struct {
	db dbinterface.Querier
}

// NewArrIDCacheStore creates a new ArrIDCacheStore
func NewArrIDCacheStore(db dbinterface.Querier) *ArrIDCacheStore {
	return &ArrIDCacheStore{db: db}
}

// ComputeTitleHash computes a SHA256 hash of the normalized title for cache lookup
func ComputeTitleHash(title string) string {
	normalized := normalizeLowerTrim(title)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

// Get retrieves a cached ID entry if it exists and hasn't expired.
//
// Expiry is compared against a bound UTC time parameter rather than SQL's
// CURRENT_TIMESTAMP. expires_at is stored by binding a Go time.Time, which the
// SQLite driver serializes as a string; CURRENT_TIMESTAMP returns its own UTC
// string in a different format, so a lexical comparison of the two only behaves
// chronologically when the process runs in UTC. Binding the comparison value
// through the same driver path (and storing expires_at in UTC, see SetWithTitles)
// makes the comparison timezone-independent on both SQLite and Postgres (#1961).
func (s *ArrIDCacheStore) Get(ctx context.Context, titleHash, contentType string) (*ArrIDCacheEntry, error) {
	query := `
		SELECT id, title_hash, content_type, arr_instance_id, imdb_id, tmdb_id, tvdb_id, tvmaze_id, titles_json, is_negative, cached_at, expires_at
		FROM arr_id_cache
		WHERE title_hash = ? AND content_type = ? AND expires_at > ?
	`

	var entry ArrIDCacheEntry
	var imdbID, titlesJSON *string
	var tmdbID, tvdbID, tvmazeID *int
	var isNegative int

	err := s.db.QueryRowContext(ctx, query, titleHash, contentType, time.Now().UTC()).Scan(
		&entry.ID,
		&entry.TitleHash,
		&entry.ContentType,
		&entry.ArrInstanceID,
		&imdbID,
		&tmdbID,
		&tvdbID,
		&tvmazeID,
		&titlesJSON,
		&isNegative,
		&entry.CachedAt,
		&entry.ExpiresAt,
	)
	if err != nil {
		return nil, err // Returns sql.ErrNoRows if not found
	}

	// Map nullable fields to ExternalIDs
	if imdbID != nil {
		entry.ExternalIDs.IMDbID = *imdbID
	}
	if tmdbID != nil {
		entry.ExternalIDs.TMDbID = *tmdbID
	}
	if tvdbID != nil {
		entry.ExternalIDs.TVDbID = *tvdbID
	}
	if tvmazeID != nil {
		entry.ExternalIDs.TVMazeID = *tvmazeID
	}
	if titlesJSON != nil {
		entry.HasTitles = true
		if err := json.Unmarshal([]byte(*titlesJSON), &entry.Titles); err != nil {
			return nil, fmt.Errorf("failed to decode arr id cache titles: %w", err)
		}
	}
	entry.IsNegative = SQLiteIntToBool(isNegative)

	return &entry, nil
}

// Set creates or updates a cache entry (upsert)
func (s *ArrIDCacheStore) Set(ctx context.Context, titleHash, contentType string, arrInstanceID *int, ids *ExternalIDs, isNegative bool, ttl time.Duration) error {
	return s.SetWithTitles(ctx, titleHash, contentType, arrInstanceID, ids, nil, isNegative, ttl)
}

// SetWithTitles creates or updates a cache entry with known ARR title aliases.
// A negative write never replaces a live positive row: concurrent lookups of the
// same title can race, and a slower no-ID result must not hide freshly resolved
// IDs for the negative TTL (#2300). Positive writes always win.
func (s *ArrIDCacheStore) SetWithTitles(ctx context.Context, titleHash, contentType string, arrInstanceID *int, ids *ExternalIDs, titles []string, isNegative bool, ttl time.Duration) error {
	// Store expires_at in UTC so the bound-UTC comparisons in Get/CleanupExpired/
	// CountValid are timezone-independent. Without .UTC() the value carries the
	// process-local offset and the cache silently misses in non-UTC zones (#1961).
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	// Prepare nullable values
	var imdbID, titlesJSON *string
	var tmdbID, tvdbID, tvmazeID *int

	if ids != nil {
		if ids.IMDbID != "" {
			imdbID = &ids.IMDbID
		}
		if ids.TMDbID > 0 {
			tmdbID = &ids.TMDbID
		}
		if ids.TVDbID > 0 {
			tvdbID = &ids.TVDbID
		}
		if ids.TVMazeID > 0 {
			tvmazeID = &ids.TVMazeID
		}
	}
	if titles != nil {
		encodedTitles, err := json.Marshal(titles)
		if err != nil {
			return fmt.Errorf("failed to encode arr id cache titles: %w", err)
		}
		titlesValue := string(encodedTitles)
		titlesJSON = &titlesValue
	}

	query := `
		INSERT INTO arr_id_cache (title_hash, content_type, arr_instance_id, imdb_id, tmdb_id, tvdb_id, tvmaze_id, titles_json, is_negative, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(title_hash, content_type) DO UPDATE SET
			arr_instance_id = excluded.arr_instance_id,
			imdb_id = excluded.imdb_id,
			tmdb_id = excluded.tmdb_id,
			tvdb_id = excluded.tvdb_id,
			tvmaze_id = excluded.tvmaze_id,
			titles_json = excluded.titles_json,
			is_negative = excluded.is_negative,
			cached_at = CURRENT_TIMESTAMP,
			expires_at = excluded.expires_at
		WHERE excluded.is_negative = 0
			OR arr_id_cache.is_negative = 1
			OR arr_id_cache.expires_at <= ?
	`

	_, err := s.db.ExecContext(ctx, query, titleHash, contentType, arrInstanceID, imdbID, tmdbID, tvdbID, tvmazeID, titlesJSON, BoolToSQLite(isNegative), expiresAt, now)
	if err != nil {
		return fmt.Errorf("failed to set arr id cache entry: %w", err)
	}

	return nil
}

// Delete removes a specific cache entry
func (s *ArrIDCacheStore) Delete(ctx context.Context, titleHash, contentType string) error {
	query := `DELETE FROM arr_id_cache WHERE title_hash = ? AND content_type = ?`

	_, err := s.db.ExecContext(ctx, query, titleHash, contentType)
	if err != nil {
		return fmt.Errorf("failed to delete arr id cache entry: %w", err)
	}

	return nil
}

// DeleteByArrInstance removes all cache entries for a specific ARR instance
func (s *ArrIDCacheStore) DeleteByArrInstance(ctx context.Context, arrInstanceID int) error {
	query := `DELETE FROM arr_id_cache WHERE arr_instance_id = ?`

	_, err := s.db.ExecContext(ctx, query, arrInstanceID)
	if err != nil {
		return fmt.Errorf("failed to delete arr id cache entries for instance: %w", err)
	}

	return nil
}

// CleanupExpired removes all expired cache entries
func (s *ArrIDCacheStore) CleanupExpired(ctx context.Context) (int64, error) {
	// Compare against a bound UTC time rather than CURRENT_TIMESTAMP; see Get (#1961).
	query := `DELETE FROM arr_id_cache WHERE expires_at <= ?`

	result, err := s.db.ExecContext(ctx, query, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired arr id cache entries: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}

// Count returns the total number of cache entries
func (s *ArrIDCacheStore) Count(ctx context.Context) (int64, error) {
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM arr_id_cache").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count arr id cache entries: %w", err)
	}
	return count, nil
}

// CountValid returns the number of non-expired cache entries
func (s *ArrIDCacheStore) CountValid(ctx context.Context) (int64, error) {
	// Compare against a bound UTC time rather than CURRENT_TIMESTAMP; see Get (#1961).
	var count int64
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM arr_id_cache WHERE expires_at > ?", time.Now().UTC()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count valid arr id cache entries: %w", err)
	}
	return count, nil
}
