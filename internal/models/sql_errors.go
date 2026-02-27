// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

func isUniqueConstraintError(err error) bool {
	if err == nil {
		return false
	}

	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) {
		return sqlErr.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	return false
}

func isCheckConstraintError(err error) bool {
	if err == nil {
		return false
	}

	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) {
		return sqlErr.Code() == sqlitelib.SQLITE_CONSTRAINT_CHECK
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23514"
	}

	return false
}

func isForeignKeyConstraintError(err error) bool {
	if err == nil {
		return false
	}

	var sqlErr *sqlite.Error
	if errors.As(err, &sqlErr) {
		return sqlErr.Code() == sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23503"
	}

	return false
}
