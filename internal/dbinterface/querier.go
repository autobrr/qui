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

type TxQuerier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	Commit() error
	Rollback() error
}

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

func DeferForeignKeyChecks(ctx context.Context, tx TxQuerier) error {
	if txWithCapabilities, ok := tx.(deferForeignKeysTx); ok {
		return txWithCapabilities.DeferForeignKeyChecks(ctx)
	}

	_, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys = ON;")
	return err
}

func BuildQueryWithPlaceholders(queryTemplate string, placeholdersPerRow int, numRows int) string {
	if placeholdersPerRow <= 0 {
		return fmt.Sprintf(queryTemplate, "")
	}
	if numRows <= 0 {
		return fmt.Sprintf(queryTemplate, "")
	}
	var sb strings.Builder
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
