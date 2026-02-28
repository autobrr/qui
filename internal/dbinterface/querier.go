// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package dbinterface provides database interfaces to avoid import cycles.
// This package has no dependencies and can be imported by both database
// implementations and models/stores.
package dbinterface

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// TxQuerier is the interface for database transaction operations.
// It is implemented by *database.Tx and provides transaction-specific query
// methods with prepared statement caching, plus transaction control methods.
type TxQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	Commit() error
	Rollback() error
}

// Querier is the centralized interface for database operations.
// It is implemented by *database.DB and provides all database capabilities
// including queries, transactions, and string interning.
type Querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	BeginTx(ctx context.Context, opts *sql.TxOptions) (TxQuerier, error)
}

type dialectProvider interface {
	Dialect() string
}

type deferForeignKeysTx interface {
	DeferForeignKeyChecks(ctx context.Context) error
}

// DialectOf returns the SQL dialect for a querier when available.
// Defaults to sqlite for backward compatibility with legacy implementations.
func DialectOf(q any) string {
	if q == nil {
		return "sqlite"
	}
	provider, ok := q.(dialectProvider)
	if !ok {
		return "sqlite"
	}
	dialect := strings.TrimSpace(provider.Dialect())
	if dialect == "" {
		return "sqlite"
	}
	return strings.ToLower(dialect)
}

// DeferForeignKeyChecks defers foreign key constraint checks for the given transaction
// until the end of the transaction. This allows operations that would normally violate
// foreign key constraints due to ordering.
func DeferForeignKeyChecks(ctx context.Context, tx TxQuerier) error {
	if txWithCapabilities, ok := tx.(deferForeignKeysTx); ok {
		return txWithCapabilities.DeferForeignKeyChecks(ctx)
	}

	_, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON;")
	return err
}

// BuildQueryWithPlaceholders builds a SQL query string with repeated placeholders
// queryTemplate should contain %s where the placeholders will be inserted
// placeholdersPerRow is the number of ? per row (e.g., 12 for file inserts)
// numRows is how many rows to repeat the placeholders for
func BuildQueryWithPlaceholders(queryTemplate string, placeholdersPerRow int, numRows int) string {
	if placeholdersPerRow <= 0 {
		return fmt.Sprintf(queryTemplate, "")
	}
	if numRows <= 0 {
		return fmt.Sprintf(queryTemplate, "")
	}

	var sb strings.Builder
	// Estimate size: each row has 2*placeholdersPerRow chars for ?, plus 2 for (), plus comma space
	totalLen := numRows*(2*placeholdersPerRow+2) + (numRows-1)*2
	sb.Grow(totalLen)
	for i := range numRows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('(')
		for j := range placeholdersPerRow {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteByte('?')
		}
		sb.WriteByte(')')
	}
	return fmt.Sprintf(queryTemplate, sb.String())
}
