// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package testdb provides isolated migrated database fixtures for tests.
package testdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/autobrr/qui/internal/database"
)

var (
	templateOnce sync.Once
	templatePath string
	errTemplate  error
)

// NewMigratedSQLite returns an isolated SQLite database with all migrations
// already applied. It avoids replaying the full migration set for every store
// test by cloning a process-local migrated template database.
func NewMigratedSQLite(t testing.TB, name string) *database.DB {
	t.Helper()

	dbPath := CloneMigratedSQLite(t, name)
	db, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("open cloned migrated test database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close cloned migrated test database: %v", err)
		}
	})

	return db
}

// NewMigratedPostgres returns an isolated migrated PostgreSQL schema when
// QUI_TEST_POSTGRES_DSN is configured, otherwise it skips the calling test.
func NewMigratedPostgres(t testing.TB, name string) *database.DB {
	t.Helper()

	baseDSN := strings.TrimSpace(os.Getenv("QUI_TEST_POSTGRES_DSN"))
	if baseDSN == "" {
		t.Skip("QUI_TEST_POSTGRES_DSN not set")
	}

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancelSetup()
	adminPool, err := pgxpool.New(setupCtx, baseDSN)
	if err != nil {
		t.Fatalf("open PostgreSQL test administrator: %v", err)
	}

	safeName := strings.ReplaceAll(sanitizeName(name), "-", "_")
	if len(safeName) > 24 {
		safeName = safeName[:24]
	}
	schemaName := fmt.Sprintf("qui_test_%s_%d", safeName, time.Now().UnixNano())
	quotedSchema := quotePostgresIdentifier(schemaName)
	if _, err := adminPool.Exec(setupCtx, "CREATE SCHEMA "+quotedSchema); err != nil {
		adminPool.Close()
		t.Fatalf("create PostgreSQL test schema: %v", err)
	}

	testDSN, err := postgresDSNWithSearchPath(baseDSN, schemaName)
	if err != nil {
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("configure PostgreSQL test schema: %v", err)
	}
	db, err := database.Open(database.OpenOptions{Engine: string(database.DialectPostgres), PostgresDSN: testDSN})
	if err != nil {
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA "+quotedSchema+" CASCADE")
		adminPool.Close()
		t.Fatalf("open migrated PostgreSQL test database: %v", err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close PostgreSQL test database: %v", err)
		}
		cleanupCtx, cancelCleanup := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCleanup()
		if _, err := adminPool.Exec(cleanupCtx, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop PostgreSQL test schema: %v", err)
		}
		adminPool.Close()
	})

	return db
}

func postgresDSNWithSearchPath(dsn, schema string) (string, error) {
	if _, err := pgxpool.ParseConfig(dsn); err != nil {
		return "", err
	}
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		escapedSchema := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(schema)
		return dsn + " search_path='" + escapedSchema + "'", nil
	}

	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func quotePostgresIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// CloneMigratedSQLite copies the migrated template database into t.TempDir and
// returns the cloned database path.
func CloneMigratedSQLite(t testing.TB, name string) string {
	t.Helper()

	src, err := migratedTemplatePath()
	if err != nil {
		t.Fatalf("prepare migrated test database template: %v", err)
	}

	dst := filepath.Join(t.TempDir(), sanitizeName(name)+".db")
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("clone migrated test database: %v", err)
	}
	return dst
}

func migratedTemplatePath() (string, error) {
	templateOnce.Do(func() {
		templatePath, errTemplate = buildMigratedTemplate()
	})
	return templatePath, errTemplate
}

func buildMigratedTemplate() (string, error) {
	dir, err := os.MkdirTemp("", "qui-migrated-testdb-*")
	if err != nil {
		return "", fmt.Errorf("create template dir: %w", err)
	}

	dbPath := filepath.Join(dir, "template.db")
	db, err := database.New(dbPath)
	if err != nil {
		return "", fmt.Errorf("create migrated template database: %w", err)
	}

	if _, err := db.Conn().ExecContext(context.Background(), "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		closeErr := db.Close()
		if closeErr != nil {
			return "", fmt.Errorf("checkpoint migrated template database: %w; close: %w", err, closeErr)
		}
		return "", fmt.Errorf("checkpoint migrated template database: %w", err)
	}
	if err := db.Close(); err != nil {
		return "", fmt.Errorf("close migrated template database: %w", err)
	}

	if err := removeSQLiteSidecars(dbPath); err != nil {
		return "", err
	}
	return dbPath, nil
}

func removeSQLiteSidecars(dbPath string) error {
	var errs []error
	for _, suffix := range []string{"-wal", "-shm"} {
		path := dbPath + suffix
		if err := os.Remove(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			errs = append(errs, fmt.Errorf("remove sqlite sidecar %s: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create clone dir: %w", err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open template: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create clone: %w", err)
	}

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy template: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close clone: %w", err)
	}

	return nil
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "qui-test"
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	sanitized := strings.Trim(b.String(), "-")
	if sanitized == "" {
		return "qui-test"
	}
	return sanitized
}
