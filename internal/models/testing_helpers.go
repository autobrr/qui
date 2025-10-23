// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"sync"
)

// mockQuerier wraps sql.DB to implement dbinterface.Querier for tests
type mockQuerier struct {
	*sql.DB
	stringCache map[string]int64
	nextID      int64
	mu          sync.Mutex
}

func newMockQuerier(db *sql.DB) *mockQuerier {
	return &mockQuerier{
		DB:          db,
		stringCache: make(map[string]int64),
		nextID:      1,
	}
}

func (m *mockQuerier) GetOrCreateStringID(ctx context.Context, value string, tx *sql.Tx) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check cache first
	if id, ok := m.stringCache[value]; ok {
		return id, nil
	}

	// Determine which executor to use
	var queryRow func(query string, args ...any) *sql.Row
	var exec func(query string, args ...any) (sql.Result, error)

	if tx != nil {
		queryRow = func(query string, args ...any) *sql.Row {
			return tx.QueryRowContext(ctx, query, args...)
		}
		exec = func(query string, args ...any) (sql.Result, error) {
			return tx.ExecContext(ctx, query, args...)
		}
	} else {
		queryRow = func(query string, args ...any) *sql.Row {
			return m.QueryRowContext(ctx, query, args...)
		}
		exec = func(query string, args ...any) (sql.Result, error) {
			return m.ExecContext(ctx, query, args...)
		}
	}

	// Check if it exists in the database
	var id int64
	err := queryRow("SELECT id FROM string_pool WHERE value = ?", value).Scan(&id)
	if err == nil {
		m.stringCache[value] = id
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Insert new string
	result, err := exec("INSERT INTO string_pool (value) VALUES (?)", value)
	if err != nil {
		return 0, err
	}

	id, err = result.LastInsertId()
	if err != nil {
		return 0, err
	}

	m.stringCache[value] = id
	return id, nil
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
