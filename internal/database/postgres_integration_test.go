// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpenPostgres(t *testing.T) {
	t.Parallel()

	baseDSN := strings.TrimSpace(os.Getenv("QUI_TEST_POSTGRES_DSN"))
	if baseDSN == "" {
		t.Skip("QUI_TEST_POSTGRES_DSN not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, baseDSN)
	if err != nil {
		t.Fatalf("open admin postgres pool: %v", err)
	}
	defer adminPool.Close()

	schemaName := fmt.Sprintf("qui_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+quoteIdent(schemaName)); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA %s CASCADE", quoteIdent(schemaName)))
	})

	testDSN := dsnWithSearchPath(t, baseDSN, schemaName)
	db, err := Open(OpenOptions{
		Engine:      string(DialectPostgres),
		PostgresDSN: testDSN,
	})
	if err != nil {
		t.Fatalf("open postgres db: %v", err)
	}
	defer db.Close()

	if got := db.Dialect(); got != string(DialectPostgres) {
		t.Fatalf("unexpected dialect: %s", got)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations").Scan(&count); err != nil {
		t.Fatalf("query migrations table: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one postgres migration row, got %d", count)
	}
}

func dsnWithSearchPath(t *testing.T, dsn string, schema string) string {
	t.Helper()

	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse postgres dsn: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
