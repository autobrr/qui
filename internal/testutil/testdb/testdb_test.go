// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

func disableBenchmarkLogs(b *testing.B) {
	b.Helper()

	original := log.Logger
	log.Logger = zerolog.Nop()
	b.Cleanup(func() {
		log.Logger = original
	})
}
