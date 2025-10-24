// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
)

// mockQuerier wraps sql.DB to implement dbinterface.Querier for tests
type mockQuerier struct {
	*sql.DB
}

func newMockQuerier(db *sql.DB) *mockQuerier {
	return &mockQuerier{
		DB: db,
	}
}

func (m *mockQuerier) GetOrCreateStringID() string {
	return "(INSERT INTO string_pool (value) VALUES (?) ON CONFLICT (value) DO UPDATE SET value = value RETURNING id)"
}

func (m *mockQuerier) GetStringByID(ctx context.Context, id int64) (string, error) {
	var value string
	err := m.QueryRowContext(ctx, "SELECT value FROM string_pool WHERE id = ?", id).Scan(&value)
	return value, err
}

func (m *mockQuerier) GetStringsByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
	result := make(map[int64]string)
	for _, id := range ids {
		value, err := m.GetStringByID(ctx, id)
		if err != nil {
			return nil, err
		}
		result[id] = value
	}
	return result, nil
}

func (m *mockQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return m.DB.BeginTx(ctx, opts)
}
