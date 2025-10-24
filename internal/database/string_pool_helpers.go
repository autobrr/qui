// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
)

// StringPoolHelper provides convenience methods for working with string interning
type StringPoolHelper struct {
	db *DB
}

// NewStringPoolHelper creates a new helper for string pool operations
func NewStringPoolHelper(db *DB) *StringPoolHelper {
	return &StringPoolHelper{db: db}
}

// BatchGetOrCreateStringIDs efficiently gets or creates multiple string IDs in a single transaction.
// This is more efficient than calling GetOrCreateStringID multiple times for bulk operations.
// Returns a map of input string to its ID.
func (h *StringPoolHelper) BatchGetOrCreateStringIDs(ctx context.Context, values []string) (map[string]int64, error) {
	if len(values) == 0 {
		return make(map[string]int64), nil
	}

	result := make(map[string]int64, len(values))

	// Use a transaction for consistency
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Prepare statements within transaction
	selectStmt, err := tx.PrepareContext(ctx, "SELECT id FROM string_pool WHERE value = ?")
	if err != nil {
		return nil, err
	}
	defer selectStmt.Close()

	insertStmt, err := tx.PrepareContext(ctx, "INSERT OR IGNORE INTO string_pool (value) VALUES (?)")
	if err != nil {
		return nil, err
	}
	defer insertStmt.Close()

	for _, value := range values {
		if value == "" {
			continue
		}

		// Check if already in result (deduplicate input)
		if _, exists := result[value]; exists {
			continue
		}

		// Try to get existing ID
		var id int64
		err := selectStmt.QueryRowContext(ctx, value).Scan(&id)
		if err == nil {
			result[value] = id
			continue
		}
		if err != sql.ErrNoRows {
			return nil, err
		}

		// Insert if not exists
		_, err = insertStmt.ExecContext(ctx, value)
		if err != nil {
			return nil, err
		}

		// Get the ID (could be from our insert or concurrent insert)
		err = selectStmt.QueryRowContext(ctx, value).Scan(&id)
		if err != nil {
			return nil, err
		}

		result[value] = id
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return result, nil
}

// StringIDPair represents a nullable string with its optional ID
type StringIDPair struct {
	Value string
	ID    *int64 // nil if value is empty or null
}

// GetOrCreateStringIDNullable handles nullable strings, returning nil ID for empty strings
func (h *StringPoolHelper) GetOrCreateStringIDNullable(ctx context.Context, value string) (*int64, error) {
	if value == "" {
		return nil, nil
	}

	var id int64
	err := h.db.QueryRowContext(ctx,
		"INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id",
		value).Scan(&id)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

// GetStringByIDNullable retrieves a string by ID, returning empty string for nil ID
func (h *StringPoolHelper) GetStringByIDNullable(ctx context.Context, id *int64) (string, error) {
	if id == nil {
		return "", nil
	}

	return h.db.GetStringByID(ctx, *id)
}

// GetStringsByIDsNullable retrieves multiple strings, handling nil IDs gracefully
func (h *StringPoolHelper) GetStringsByIDsNullable(ctx context.Context, ids []*int64) (map[int64]string, error) {
	// Filter out nil IDs
	validIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id != nil {
			validIDs = append(validIDs, *id)
		}
	}

	if len(validIDs) == 0 {
		return make(map[int64]string), nil
	}

	return h.db.GetStringsByIDs(ctx, validIDs)
}

// StringReference is a convenience type for working with interned strings in structs
type StringReference struct {
	ID    int64
	Value string // Cached value, may be empty until loaded
}

// NewStringReference creates a reference from a known ID
func NewStringReference(id int64) StringReference {
	return StringReference{ID: id}
}

// NewStringReferenceWithValue creates a reference with both ID and cached value
func NewStringReferenceWithValue(id int64, value string) StringReference {
	return StringReference{ID: id, Value: value}
}

// Load populates the Value field from the database if not already loaded
func (s *StringReference) Load(ctx context.Context, db *DB) error {
	if s.Value != "" {
		return nil // Already loaded
	}

	value, err := db.GetStringByID(ctx, s.ID)
	if err != nil {
		return err
	}

	s.Value = value
	return nil
}

// GetOrCreateFromValue creates a StringReference from a string value
func (h *StringPoolHelper) GetOrCreateFromValue(ctx context.Context, value string) (StringReference, error) {
	var id int64
	err := h.db.QueryRowContext(ctx,
		"INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id",
		value).Scan(&id)
	if err != nil {
		return StringReference{}, err
	}

	return StringReference{ID: id, Value: value}, nil
}

// StringPool statistics for monitoring
type StringPoolStats struct {
	TotalStrings    int64
	TotalSizeBytes  int64
	AverageLength   float64
	ReferenceCounts map[string]int // Top referenced strings
}

// GetStringPoolStats returns statistics about the string pool for monitoring
func (h *StringPoolHelper) GetStringPoolStats(ctx context.Context) (*StringPoolStats, error) {
	stats := &StringPoolStats{
		ReferenceCounts: make(map[string]int),
	}

	// Get total count and size
	err := h.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COALESCE(SUM(LENGTH(value)), 0)
		FROM string_pool
	`).Scan(&stats.TotalStrings, &stats.TotalSizeBytes)
	if err != nil {
		return nil, err
	}

	if stats.TotalStrings > 0 {
		stats.AverageLength = float64(stats.TotalSizeBytes) / float64(stats.TotalStrings)
	}

	// Get top 10 most referenced strings
	// This is an expensive query, so it's optional
	// Uncomment if needed for monitoring
	/*
		rows, err := h.db.QueryContext(ctx, `
			SELECT sp.value, COUNT(*) as ref_count
			FROM (
				SELECT torrent_hash_id as id FROM torrent_files_cache WHERE torrent_hash_id IS NOT NULL
				UNION ALL
				SELECT name_id FROM torrent_files_cache WHERE name_id IS NOT NULL
				UNION ALL
				SELECT torrent_hash_id FROM torrent_files_sync WHERE torrent_hash_id IS NOT NULL
				UNION ALL
				SELECT torrent_hash_id FROM instance_backup_items WHERE torrent_hash_id IS NOT NULL
				UNION ALL
				SELECT name_id FROM instance_backup_items WHERE name_id IS NOT NULL
				UNION ALL
				SELECT category_id FROM instance_backup_items WHERE category_id IS NOT NULL
				UNION ALL
				SELECT tags_id FROM instance_backup_items WHERE tags_id IS NOT NULL
				UNION ALL
				SELECT error_type_id FROM instance_errors WHERE error_type_id IS NOT NULL
			) refs
			JOIN string_pool sp ON refs.id = sp.id
			GROUP BY sp.value
			ORDER BY ref_count DESC
			LIMIT 10
		`)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var value string
			var count int
			if err := rows.Scan(&value, &count); err != nil {
				return nil, err
			}
			stats.ReferenceCounts[value] = count
		}
	*/

	return stats, nil
}
