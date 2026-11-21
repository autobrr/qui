// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SQLite has SQLITE_MAX_VARIABLE_NUMBER limit (default 999, but can be higher).
// Use a conservative value so batch queries also stay portable in tests and tooling.
const maxParams = 900

func InternStrings(ctx context.Context, tx TxQuerier, values ...string) ([]int64, error) {
	if len(values) == 0 {
		return []int64{}, nil
	}

	for i, value := range values {
		if value == "" {
			return nil, fmt.Errorf("value at index %d is empty", i)
		}
	}

	uniqueValues := make(map[string]struct{}, len(values))
	for _, v := range values {
		uniqueValues[v] = struct{}{}
	}

	valuesList := make([]string, 0, len(uniqueValues))
	for v := range uniqueValues {
		valuesList = append(valuesList, v)
	}

	existingIDs, err := GetStringID(ctx, tx, valuesList...)
	if err != nil {
		return nil, err
	}

	valueToID := make(map[string]int64, len(valuesList))
	var missing []string
	for i, id := range existingIDs {
		if id.Valid {
			valueToID[valuesList[i]] = id.Int64
		} else {
			missing = append(missing, valuesList[i])
		}
	}

	if len(missing) > 0 {
		slices.Sort(missing)

		queryTemplate := "INSERT INTO string_pool (value) VALUES %s ON CONFLICT(value) DO NOTHING"
		for i := 0; i < len(missing); i += maxParams {
			end := min(i+maxParams, len(missing))
			chunk := missing[i:end]

			args := make([]any, len(chunk))
			for j, v := range chunk {
				args[j] = v
			}

			query := BuildQueryWithPlaceholders(queryTemplate, 1, len(chunk))
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return nil, fmt.Errorf("failed to batch insert strings: %w", err)
			}
		}

		insertedIDs, err := GetStringID(ctx, tx, missing...)
		if err != nil {
			return nil, err
		}
		for i, id := range insertedIDs {
			if !id.Valid {
				return nil, fmt.Errorf("failed to get ID for interned string %q", missing[i])
			}
			valueToID[missing[i]] = id.Int64
		}
	}

	result := make([]int64, len(values))
	for i, v := range values {
		id, ok := valueToID[v]
		if !ok {
			return nil, fmt.Errorf("failed to get ID for interned string %q", v)
		}
		result[i] = id
	}

	return result, nil
}

func InternStringNullable(ctx context.Context, tx TxQuerier, values ...*string) ([]sql.NullInt64, error) {
	if len(values) == 0 {
		return []sql.NullInt64{}, nil
	}

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

	if len(nonEmptyValues) == 0 {
		return results, nil
	}

	ids, err := InternStrings(ctx, tx, nonEmptyValues...)
	if err != nil {
		return nil, err
	}

	for i, pos := range positions {
		results[pos] = sql.NullInt64{Int64: ids[i], Valid: true}
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
	// SELECT first: a conflicting INSERT would still burn a Postgres sequence
	// value (see InternStrings).
	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ''").Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("failed to get empty string ID: %w", err)
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO string_pool (value) VALUES ('') ON CONFLICT(value) DO NOTHING")
	if err != nil {
		return 0, fmt.Errorf("failed to ensure empty string in string_pool: %w", err)
	}

	err = tx.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ''").Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("failed to get empty string ID: %w", err)
	}

	return id, nil
}

func GetStringID(ctx context.Context, tx TxQuerier, values ...string) ([]sql.NullInt64, error) {
	if len(values) == 0 {
		return []sql.NullInt64{}, nil
	}

	if len(values) == 1 {
		return getSingleStringID(ctx, tx, values[0])
	}

	results := make([]sql.NullInt64, len(values))

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

	if len(nonEmptyValues) == 0 {
		return results, nil
	}

	uniqueValues := make(map[string][]int)
	for i, v := range nonEmptyValues {
		resultIdx := positions[i]
		uniqueValues[v] = append(uniqueValues[v], resultIdx)
	}

	valuesList := make([]string, 0, len(uniqueValues))
	for v := range uniqueValues {
		valuesList = append(valuesList, v)
	}

	valueToID := make(map[string]int64, len(valuesList))

	for i := 0; i < len(valuesList); i += maxParams {
		end := min(i+maxParams, len(valuesList))
		if err := queryStringIDChunk(ctx, tx, valuesList[i:end], valueToID); err != nil {
			return nil, err
		}
	}

	for value, resultIndices := range uniqueValues {
		if id, exists := valueToID[value]; exists {
			for _, idx := range resultIndices {
				results[idx] = sql.NullInt64{Int64: id, Valid: true}
			}
		}
	}

	return results, nil
}

func getSingleStringID(ctx context.Context, tx TxQuerier, value string) ([]sql.NullInt64, error) {
	if value == "" {
		return []sql.NullInt64{{Valid: false}}, nil
	}

	var id int64
	err := tx.QueryRowContext(ctx, "SELECT id FROM string_pool WHERE value = ?", value).Scan(&id)
	if err != nil {
		if err == sql.ErrNoRows {
			return []sql.NullInt64{{Valid: false}}, nil
		}
		return nil, fmt.Errorf("failed to get string ID from pool: %w", err)
	}

	return []sql.NullInt64{{Int64: id, Valid: true}}, nil
}

func queryStringIDChunk(ctx context.Context, tx TxQuerier, chunk []string, valueToID map[string]int64) error {
	args := make([]any, len(chunk))
	for j, v := range chunk {
		args[j] = v
	}

	var sb strings.Builder
	const queryPrefix = "SELECT id, value FROM string_pool WHERE value IN ("
	sb.Grow(len(queryPrefix) + (len(chunk) * 4) + 1)
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
		return fmt.Errorf("failed to query string pool: %w", err)
	}

	for rows.Next() {
		var id int64
		var value string
		if err := rows.Scan(&id, &value); err != nil {
			rows.Close()
			return fmt.Errorf("failed to scan string pool row: %w", err)
		}
		valueToID[value] = id
	}

	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("error iterating string pool rows: %w", err)
	}
	rows.Close()
	return nil
}
