// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dbinterface provides database interfaces to avoid import cycles.
// This package has no dependencies and can be imported by both database
// implementations and models/stores.
package dbinterface

import (
	"context"
	"database/sql"
)

// Querier is the centralized interface for database operations.
// It is implemented by *database.DB and provides all database capabilities
// including queries, transactions, and string interning.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
	GetOrCreateStringID() string
	GetStringByID(ctx context.Context, id int64) (string, error)
	GetStringsByIDs(ctx context.Context, ids []int64) (map[int64]string, error)
}
