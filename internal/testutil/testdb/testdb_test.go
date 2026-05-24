// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
)

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
	if _, err := migratedTemplatePath(); err != nil {
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

func TestNewIsFast(t *testing.T) {
	disableTestLogs(t)
	NewMigratedSQLite(t, "fast-warm")

	start := time.Now()
	NewMigratedSQLite(t, "fast-second")
	elapsed := time.Since(start)

	if elapsed > 250*time.Millisecond {
		t.Fatalf("second migrated sqlite creation took %s, want under 250ms", elapsed)
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
