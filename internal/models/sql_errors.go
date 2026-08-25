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

	if sqlErr, ok := errors.AsType[*sqlite.Error](err); ok {
		return sqlErr.Code() == sqlitelib.SQLITE_CONSTRAINT_UNIQUE
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23505"
	}

	return false
}

func isCheckConstraintError(err error) bool {
	if err == nil {
		return false
	}

	if sqlErr, ok := errors.AsType[*sqlite.Error](err); ok {
		return sqlErr.Code() == sqlitelib.SQLITE_CONSTRAINT_CHECK
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23514"
	}

	return false
}

func isForeignKeyConstraintError(err error) bool {
	if err == nil {
		return false
	}

	if sqlErr, ok := errors.AsType[*sqlite.Error](err); ok {
		return sqlErr.Code() == sqlitelib.SQLITE_CONSTRAINT_FOREIGNKEY
	}

	if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
		return pgErr.Code == "23503"
	}

	return false
}
