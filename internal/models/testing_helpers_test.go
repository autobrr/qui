// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"

	"github.com/autobrr/qui/internal/dbinterface"
)

// mockQuerier wraps sql.DB to implement dbinterface.Querier for tests
type mockQuerier struct {
	*sql.DB
}

// mockTx wraps sql.Tx to implement dbinterface.TxQuerier for tests
type mockTx struct {
	*sql.Tx
}

func (m *mockTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return m.Tx.ExecContext(ctx, query, args...)
}

func (m *mockTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return m.Tx.QueryContext(ctx, query, args...)
}

func (m *mockTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return m.Tx.QueryRowContext(ctx, query, args...)
}

func (m *mockTx) Commit() error {
	return m.Tx.Commit()
}

func (m *mockTx) Rollback() error {
	return m.Tx.Rollback()
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

func (m *mockQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := m.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &mockTx{Tx: tx}, nil
}
