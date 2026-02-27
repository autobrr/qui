// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SQLite has SQLITE_MAX_VARIABLE_NUMBER limit (default 999, but can be higher).
// Use a conservative value so batch queries also stay portable in tests and tooling.
const maxParams = 900

// InternStrings interns one or more string values efficiently and returns their IDs.
// This is designed for use within transactions.
// All values are required (non-empty). Returns error if any value is empty.
//
// Performance: Uses INSERT + SELECT instead of INSERT...RETURNING for massive speedup.
// RETURNING causes expensive B-tree traversals. For 180k torrents, this optimization
// provides 5-10x faster string interning by separating insert from ID retrieval.
// For multiple strings, uses batch operations with deduplication for optimal performance.
func InternStrings(ctx context.Context, tx TxQuerier, values ...string) ([]int64, error) {
	if len(values) == 0 {
		return []int64{}, nil
	}

	// Fast path for single string.
	if len(values) == 1 {
		return internSingleString(ctx, tx, values[0])
	}

	// Batch path for multiple strings
	// Validate all values first
	for i, value := range values {
		if value == "" {
			return nil, fmt.Errorf("value at index %d is empty", i)
		}
	}

	// Deduplicate input values and track original positions
	uniqueValues := make(map[string][]int) // value -> list of indices
	for i, v := range values {
		uniqueValues[v] = append(uniqueValues[v], i)
	}

	// Build list of unique values
	valuesList := make([]string, 0, len(uniqueValues))
	for v := range uniqueValues {
		valuesList = append(valuesList, v)
	}

	// Pre-build the query template for full chunks to avoid repeated string building in hot path
	queryTemplate := "INSERT INTO string_pool (value) VALUES %s ON CONFLICT(value) DO NOTHING"
	fullQuery := BuildQueryWithPlaceholders(queryTemplate, 1, maxParams)

	for i := 0; i < len(valuesList); i += maxParams {
		end := i + maxParams
		if end > len(valuesList) {
			end = len(valuesList)
		}
		chunk := valuesList[i:end]

		// Build args for this chunk
		args := make([]any, len(chunk))
		for j, v := range chunk {
			args[j] = v
		}

		// Use pre-built query for full chunks, build new one only for smaller final chunk
		query := fullQuery
		if len(chunk) < maxParams {
			query = BuildQueryWithPlaceholders(queryTemplate, 1, len(chunk))
		}

		_, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return nil, fmt.Errorf("failed to batch insert strings: %w", err)
		}
	}

	// Step 2: Get all IDs using GetStringID
	ids, err := GetStringID(ctx, tx, values...)
	if err != nil {
		return nil, err
	}

	// Verify all IDs are valid (they should be after ON CONFLICT DO NOTHING insert)
	result := make([]int64, len(ids))
	for i, id := range ids {
		if !id.Valid {
			return nil, fmt.Errorf("failed to get ID for interned string %q", values[i])
		}
		result[i] = id.Int64
	}

	return result, nil
}

func internSingleString(ctx context.Context, tx TxQuerier, value string) ([]int64, error) {
	if value == "" {
		return nil, errors.New("value at index 0 is empty")
	}

	_, err := tx.ExecContext(ctx,
		"INSERT INTO string_pool (value) VALUES (?) ON CONFLICT(value) DO NOTHING",
		value)
	if err != nil {
		return nil, err
	}

	ids, err := GetStringID(ctx, tx, value)
	if err != nil {
		return nil, err
	}
	if !ids[0].Valid {
		return nil, fmt.Errorf("failed to get ID for interned string %q", value)
	}
	return []int64{ids[0].Int64}, nil
}

// InternStringNullable interns one or more optional string values and returns their IDs as sql.NullInt64.
// Returns sql.NullInt64{Valid: false} for any value pointer that is nil or points to an empty string.
// This is designed for use within transactions.
//
// Performance: For a single string, uses a fast-path. For multiple strings, collects non-empty values
// and delegates to InternStrings for efficient batch processing.
func InternStringNullable(ctx context.Context, tx TxQuerier, values ...*string) ([]sql.NullInt64, error) {
	if len(values) == 0 {
		return []sql.NullInt64{}, nil
	}

	// Fast path for single string
	if len(values) == 1 {
		if values[0] == nil || *values[0] == "" {
			return []sql.NullInt64{{Valid: false}}, nil
		}

		ids, err := InternStrings(ctx, tx, *values[0])
		if err != nil {
			return nil, err
		}

		return []sql.NullInt64{{Int64: ids[0], Valid: true}}, nil
	}

	// Batch path: collect non-empty values and track their positions
	results := make([]sql.NullInt64, len(values))
	var nonEmptyValues []string
	var positions []int

	for i, v := range values {
		if v == nil || *v == "" {
			results[i] = sql.NullInt64{Valid: false}
			continue
		}
		nonEmptyValues = append(nonEmptyValues, *v)
		positions = append(positions, i)
	}

	// If no non-empty values, return early
	if len(nonEmptyValues) == 0 {
		return results, nil
	}

	// Intern all non-empty values (InternStrings handles deduplication internally)
	ids, err := InternStrings(ctx, tx, nonEmptyValues...)
	if err != nil {
		return nil, err
	}

	// Map IDs back to original positions
	for i, pos := range positions {
		results[pos] = sql.NullInt64{Int64: ids[i], Valid: true}
	}

	return results, nil
}

// GetString retrieves one or more string values from the string_pool by their IDs.
// This is designed for use within transactions.
// Returns strings in the same order as the input IDs.
func GetString(ctx context.Context, tx TxQuerier, ids ...int64) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}

	// Fast path for single ID
	if len(ids) == 1 {
		var value string
		err := tx.QueryRowContext(ctx, "SELECT value FROM string_pool WHERE id = ?", ids[0]).Scan(&value)
		if err != nil {
			return nil, fmt.Errorf("failed to get string from pool: %w", err)
		}
		return []string{value}, nil
	}

	// Batch path for multiple IDs
	// SQLite has SQLITE_MAX_VARIABLE_NUMBER limit (default 999)
	// Process in chunks to avoid hitting this limit
	results := make([]string, len(ids))
	idToPositions := make(map[int64][]int, len(ids))
	for i, id := range ids {
		idToPositions[id] = append(idToPositions[id], i)
	}

	for i := 0; i < len(ids); i += maxParams {
		end := i + maxParams
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]

		// Build args for this chunk
		args := make([]any, len(chunk))
		for j, id := range chunk {
			args[j] = id
		}

		// Build IN clause: id IN (?,?,?)
		var sb strings.Builder
		const queryPrefix = "SELECT id, value FROM string_pool WHERE id IN ("
		sb.Grow(len(queryPrefix) + (len(chunk) * 4) + 1) // preallocate
		sb.WriteString(queryPrefix)
		for j := range chunk {
			if j > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("?")
		}
		sb.WriteString(")")

		rows, err := tx.QueryContext(ctx, sb.String(), args...)
		if err != nil {
			return nil, fmt.Errorf("failed to query string pool: %w", err)
		}

		for rows.Next() {
			var id int64
			var value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan string pool row: %w", err)
			}
			if positions, exists := idToPositions[id]; exists {
				for _, idx := range positions {
					results[idx] = value
				}
			}
		}

		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("error iterating string pool rows: %w", err)
		}
		rows.Close()
	}

	return results, nil
}

// InternEmptyString ensures the empty string exists in string_pool and returns its ID.
// This is needed for special cases like localhost bypass auth where an empty username
// is a valid, intentional value (not NULL). The empty string is created if it doesn't exist.
//
// Unlike InternStrings which rejects empty strings (treating them as invalid input),
// this function specifically handles the case where empty string is a meaningful value.
func InternEmptyString(ctx context.Context, tx TxQuerier) (int64, error) {
	// Ensure empty string exists
	_, err := tx.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES ('') ON CONFLICT(value) DO NOTHING")
	if err != nil {
		return 0, fmt.Errorf("failed to ensure empty string in string_pool: %w", err)
	}

	// Get the ID
	var id int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ''").Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to get empty string ID: %w", err)
	}

	return id, nil
}

// GetStringID retrieves the IDs of string values from the string_pool without creating them.
// Returns sql.NullInt64{Valid: false} for strings that do not exist.
// This is designed for use within transactions.
// For multiple strings, uses batch operations for optimal performance.
func GetStringID(ctx context.Context, tx TxQuerier, values ...string) ([]sql.NullInt64, error) {
	if len(values) == 0 {
		return []sql.NullInt64{}, nil
	}

	// Fast path for single string
	if len(values) == 1 {
		if values[0] == "" {
			return []sql.NullInt64{{Valid: false}}, nil
		}

		var id int64
		err := tx.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", values[0]).Scan(&id)
		if err != nil {
			if err == sql.ErrNoRows {
				return []sql.NullInt64{{Valid: false}}, nil
			}
			return nil, fmt.Errorf("failed to get string ID from pool: %w", err)
		}

		return []sql.NullInt64{{Int64: id, Valid: true}}, nil
	}

	// Batch path for multiple strings
	results := make([]sql.NullInt64, len(values))

	// Filter out empty strings and track positions
	var nonEmptyValues []string
	var positions []int
	for i, v := range values {
		if v == "" {
			results[i] = sql.NullInt64{Valid: false}
			continue
		}
		nonEmptyValues = append(nonEmptyValues, v)
		positions = append(positions, i)
	}

	// If no non-empty values, return early
	if len(nonEmptyValues) == 0 {
		return results, nil
	}

	// Deduplicate non-empty values and track their positions
	uniqueValues := make(map[string][]int) // value -> list of result indices
	for i, v := range nonEmptyValues {
		resultIdx := positions[i]
		uniqueValues[v] = append(uniqueValues[v], resultIdx)
	}

	// Build list of unique values
	valuesList := make([]string, 0, len(uniqueValues))
	for v := range uniqueValues {
		valuesList = append(valuesList, v)
	}

	// SQLite has SQLITE_MAX_VARIABLE_NUMBER limit (default 999)
	// Process in chunks to avoid hitting this limit
	valueToID := make(map[string]int64, len(valuesList))

	for i := 0; i < len(valuesList); i += maxParams {
		end := i + maxParams
		if end > len(valuesList) {
			end = len(valuesList)
		}
		chunk := valuesList[i:end]

		// Build args for this chunk
		args := make([]any, len(chunk))
		for j, v := range chunk {
			args[j] = v
		}

		// Build IN clause: value IN (?,?,?)
		var sb strings.Builder
		const queryPrefix = "SELECT id, value FROM string_pool WHERE value IN ("
		sb.Grow(len(queryPrefix) + (len(chunk) * 4) + 1) // preallocate
		sb.WriteString(queryPrefix)
		for j := range chunk {
			if j > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("?")
		}
		sb.WriteString(")")

		rows, err := tx.QueryContext(ctx, sb.String(), args...)
		if err != nil {
			return nil, fmt.Errorf("failed to query string pool: %w", err)
		}

		for rows.Next() {
			var id int64
			var value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return nil, fmt.Errorf("failed to scan string pool row: %w", err)
			}
			valueToID[value] = id
		}

		if err = rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("error iterating string pool rows: %w", err)
		}
		rows.Close()
	}

	// Map IDs back to result positions
	for value, resultIndices := range uniqueValues {
		if id, exists := valueToID[value]; exists {
			for _, idx := range resultIndices {
				results[idx] = sql.NullInt64{Int64: id, Valid: true}
			}
		}
		// If value doesn't exist, results[idx] remains {Valid: false}
	}

	return results, nil
}
