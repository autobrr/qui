// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
)

func TestPostgresDSNWithSearchPath(t *testing.T) {
	const schema = "qui_test_schema"
	tests := []struct {
		name         string
		dsn          string
		wantPassword string
		wantErr      bool
	}{
		{
			name:         "URL",
			dsn:          "postgres://tester:url%20secret@localhost:5432/qui?sslmode=disable&application_name=suite&search_path=old",
			wantPassword: "url secret",
		},
		{
			name:         "keyword value",
			dsn:          "host=localhost port=5432 user=tester password='keyword secret' dbname=qui sslmode=disable application_name=suite search_path=old",
			wantPassword: "keyword secret",
		},
		{
			name:    "malformed URL",
			dsn:     "postgres://%zz",
			wantErr: true,
		},
		{
			name:    "malformed keyword value",
			dsn:     "host='unterminated",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := postgresDSNWithSearchPath(tt.dsn, schema)
			if tt.wantErr {
				if err == nil {
					t.Fatal("postgresDSNWithSearchPath() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("postgresDSNWithSearchPath() error = %v", err)
			}

			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("parse modified DSN: %v", err)
			}
			if got := config.ConnConfig.RuntimeParams["search_path"]; got != schema {
				t.Fatalf("search_path = %q, want %q", got, schema)
			}
			if got := config.ConnConfig.RuntimeParams["application_name"]; got != "suite" {
				t.Fatalf("application_name = %q, want suite", got)
			}
			if got := config.ConnConfig.Password; got != tt.wantPassword {
				t.Fatalf("password = %q, want %q", got, tt.wantPassword)
			}
		})
	}
}

func BenchmarkFullMigrationTestDB(b *testing.B) {
	disableBenchmarkLogs(b)
	parent := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := database.New(filepath.Join(parent, fmt.Sprintf("full-%d.db", i)))
		if err != nil {
			b.Fatalf("create full migration db: %v", err)
		}
		if err := db.Close(); err != nil {
			b.Fatalf("close full migration db: %v", err)
		}
	}
}

func BenchmarkClonedMigratedTestDB(b *testing.B) {
	disableBenchmarkLogs(b)
	templatePath, err := migratedTemplatePath()
	if err != nil {
		b.Fatalf("prepare migrated template: %v", err)
	}

	parent := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dbPath := filepath.Join(parent, fmt.Sprintf("clone-%d.db", i))
		if err := copyFile(templatePath, dbPath); err != nil {
			b.Fatalf("clone migrated template: %v", err)
		}
		db, err := database.New(dbPath)
		if err != nil {
			b.Fatalf("open cloned migration db: %v", err)
		}
		if err := db.Close(); err != nil {
			b.Fatalf("close cloned migration db: %v", err)
		}
	}
}

func TestRemoveSQLiteSidecars(t *testing.T) {
	t.Run("ignores missing sidecars", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "template.db")

		if err := removeSQLiteSidecars(dbPath); err != nil {
			t.Fatalf("remove missing sidecars: %v", err)
		}
	})

	t.Run("removes existing sidecars", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "template.db")
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.WriteFile(dbPath+suffix, []byte("sidecar"), 0o600); err != nil {
				t.Fatalf("create sidecar %s: %v", suffix, err)
			}
		}

		if err := removeSQLiteSidecars(dbPath); err != nil {
			t.Fatalf("remove sidecars: %v", err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
				t.Fatalf("sidecar %s still exists: %v", suffix, err)
			}
		}
	})
}

func TestTemplatePathIsCached(t *testing.T) {
	disableTestLogs(t)

	first, err := migratedTemplatePath()
	if err != nil {
		t.Fatalf("first migrated template path: %v", err)
	}

	second, err := migratedTemplatePath()
	if err != nil {
		t.Fatalf("second migrated template path: %v", err)
	}

	if first != second {
		t.Fatalf("migrated template path differs across calls: first=%q, second=%q", first, second)
	}
}

func TestNewIsIsolated(t *testing.T) {
	disableTestLogs(t)
	ctx := context.Background()
	first := NewMigratedSQLite(t, "isolated-first")
	second := NewMigratedSQLite(t, "isolated-second")

	insertIsolationProbe(ctx, t, first, "first")
	insertIsolationProbe(ctx, t, second, "second")

	assertIsolationProbe(ctx, t, first, "first")
	assertIsolationProbe(ctx, t, second, "second")
}

func TestNewParallel(t *testing.T) {
	disableTestLogs(t)
	for i := range 8 {
		t.Run(fmt.Sprintf("db-%d", i), func(t *testing.T) {
			t.Parallel()

			db := NewMigratedSQLite(t, t.Name())
			if err := db.Conn().PingContext(context.Background()); err != nil {
				t.Fatalf("ping migrated sqlite: %v", err)
			}
		})
	}
}

func insertIsolationProbe(ctx context.Context, t *testing.T, db *database.DB, value string) {
	t.Helper()

	if _, err := db.Conn().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS isolation_probe (value TEXT NOT NULL)"); err != nil {
		t.Fatalf("create isolation probe table: %v", err)
	}
	if _, err := db.Conn().ExecContext(ctx, "INSERT INTO isolation_probe (value) VALUES (?)", value); err != nil {
		t.Fatalf("insert isolation probe: %v", err)
	}
}

func assertIsolationProbe(ctx context.Context, t *testing.T, db *database.DB, want string) {
	t.Helper()

	var total int
	var value string
	if err := db.Conn().QueryRowContext(ctx, "SELECT COUNT(*), MAX(value) FROM isolation_probe").Scan(&total, &value); err != nil {
		t.Fatalf("query isolation probe: %v", err)
	}
	if total != 1 || value != want {
		t.Fatalf("isolation probe = count %d, value %q; want count 1, value %q", total, value, want)
	}
}

func disableTestLogs(t *testing.T) {
	t.Helper()

	original := log.Logger
	log.Logger = zerolog.Nop()
	t.Cleanup(func() {
		log.Logger = original
	})
}

func disableBenchmarkLogs(b *testing.B) {
	b.Helper()

	original := log.Logger
	log.Logger = zerolog.Nop()
	b.Cleanup(func() {
		log.Logger = original
	})
}
