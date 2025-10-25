// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"context"
	"database/sql"
	"fmt"
)

// InternString interns a single string value and returns its ID.
// This is designed for use within transactions.
// For required (non-nullable) string fields.
func InternString(ctx context.Context, tx TxQuerier, value string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx,
		"INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id",
		value).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// InternStringNullable interns an optional string value and returns its ID as sql.NullInt64.
// Returns sql.NullInt64{Valid: false} if the value pointer is nil or the string is empty.
// This is designed for use within transactions.
func InternStringNullable(ctx context.Context, tx TxQuerier, value *string) (sql.NullInt64, error) {
	if value == nil || *value == "" {
		return sql.NullInt64{Valid: false}, nil
	}

	id, err := InternString(ctx, tx, *value)
	if err != nil {
		return sql.NullInt64{}, err
	}

	return sql.NullInt64{Int64: id, Valid: true}, nil
}

// InternStrings interns multiple string values in sequence and returns their IDs.
// This is designed for use within transactions.
// All values are required (non-empty). Returns error if any value is empty.
func InternStrings(ctx context.Context, tx TxQuerier, values ...string) ([]int64, error) {
	ids := make([]int64, len(values))
	for i, value := range values {
		if value == "" {
			return nil, fmt.Errorf("value at index %d is empty", i)
		}
		id, err := InternString(ctx, tx, value)
		if err != nil {
			return nil, fmt.Errorf("failed to intern value at index %d: %w", i, err)
		}
		ids[i] = id
	}
	return ids, nil
}

// GetString retrieves a string value from the string_pool by its ID.
// This is designed for use within transactions.
func GetString(ctx context.Context, tx TxQuerier, id int64) (string, error) {
	var value string
	err := tx.QueryRowContext(ctx, "SELECT value FROM string_pool WHERE id = ?", id).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("failed to get string from pool: %w", err)
	}
	return value, nil
}
