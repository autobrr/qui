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
// It is implemented by *sql.DB, *sql.Tx, and *database.DB.
// This allows stores and repositories to accept any of these types
// and enables transaction support without code duplication.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// TxBeginner is an interface for types that can begin transactions.
// It is implemented by *sql.DB and *database.DB.
type TxBeginner interface {
	Querier
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// StringInterning provides methods for string interning operations.
// This interface is implemented by *database.DB to support efficient
// string storage in the string_pool table.
type StringInterning interface {
	GetOrCreateStringID(ctx context.Context, value string) (int64, error)
	GetStringByID(ctx context.Context, id int64) (string, error)
	GetStringsByIDs(ctx context.Context, ids []int64) (map[int64]string, error)
}

// DBWithStringInterning combines all database capabilities including string interning.
type DBWithStringInterning interface {
	TxBeginner
	StringInterning
}
