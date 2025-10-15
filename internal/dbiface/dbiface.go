package dbiface

import (
    "context"
    "database/sql"
)

// DBLike is a minimal interface for database operations used by stores.
// It is implemented by *sql.DB and by the project's *DB wrapper.
type DBLike interface {
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}
