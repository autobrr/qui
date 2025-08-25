// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rs/zerolog/log"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	conn *sql.DB
}

func New(databasePath string) (*DB, error) {
	log.Info().Msgf("Initializing database at: %s", databasePath)

	// Ensure the directory exists
	dir := filepath.Dir(databasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory %s: %w", dir, err)
	}
	log.Debug().Msgf("Database directory ensured: %s", dir)

	// Open connection for migrations with single connection only
	// This prevents any connection pool issues during schema changes
	conn, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", databasePath, err)
	}

	// CRITICAL: Use only 1 connection during migrations to prevent stale schema issues
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	log.Debug().Msg("Database connection opened for migrations")

	// Enable foreign keys and WAL mode for better performance
	if _, err := conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode = WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Set busy timeout to 10 seconds
	if _, err := conn.Exec("PRAGMA busy_timeout = 10000"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	db := &DB{
		conn: conn,
	}

	// Run migrations with single connection
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// After migrations, allow connection pooling for normal operations
	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(25)
	// 5 minute lifetime prevents stale connections from accumulating
	conn.SetConnMaxLifetime(5 * time.Minute)

	// Verify database file was created
	if _, err := os.Stat(databasePath); err != nil {
		conn.Close()
		return nil, fmt.Errorf("database file was not created at %s: %w", databasePath, err)
	}
	log.Info().Msgf("Database initialized successfully at: %s", databasePath)

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) Conn() *sql.DB {
	return db.conn
}

func (db *DB) migrate() error {
	ctx := context.Background()

	// Create migrations table if it doesn't exist
	if _, err := db.conn.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Get all migration files
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Sort migration files by name
	var files []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)

	// Find pending migrations
	pendingMigrations, err := db.findPendingMigrations(ctx, files)
	if err != nil {
		return fmt.Errorf("failed to find pending migrations: %w", err)
	}

	if len(pendingMigrations) == 0 {
		log.Debug().Msg("No pending migrations")
		return nil
	}

	// Apply all pending migrations in a single transaction
	if err := db.applyAllMigrations(ctx, pendingMigrations); err != nil {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}

	return nil
}

func (db *DB) findPendingMigrations(ctx context.Context, allFiles []string) ([]string, error) {
	var pendingMigrations []string

	for _, filename := range allFiles {
		var count int
		err := db.conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = ?", filename).Scan(&count)
		if err != nil {
			return nil, fmt.Errorf("failed to check migration status for %s: %w", filename, err)
		}

		if count == 0 {
			pendingMigrations = append(pendingMigrations, filename)
		}
	}

	return pendingMigrations, nil
}

func (db *DB) applyAllMigrations(ctx context.Context, migrations []string) error {
	// Begin single transaction for all migrations
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	// defer Rollback - will be no-op if Commit succeeds
	defer tx.Rollback()

	// Apply each migration within the transaction
	for _, filename := range migrations {
		// Read migration file
		content, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", filename, err)
		}

		// Execute migration
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", filename, err)
		}

		// Record migration
		if _, err := tx.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES (?)", filename); err != nil {
			return fmt.Errorf("failed to record migration %s: %w", filename, err)
		}

	}

	// Commit all migrations at once
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migrations: %w", err)
	}

	log.Info().Msgf("Applied %d migrations successfully", len(migrations))
	return nil
}
