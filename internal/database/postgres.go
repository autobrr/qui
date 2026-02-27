// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
	"github.com/rs/zerolog/log"

	// Register pgx as database/sql driver.
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed postgres_migrations/*.sql
var postgresMigrationsFS embed.FS

func newPostgres(dsn string, opts OpenOptions) (*DB, error) {
	log.Info().Msg("Initializing postgres database")

	writerConn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres connection: %w", err)
	}

	maxOpenConns := opts.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 25
	}
	maxIdleConns := opts.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 5
	}
	connMaxLifetime := opts.ConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = 5 * time.Minute
	}

	writerConn.SetMaxOpenConns(maxOpenConns)
	writerConn.SetMaxIdleConns(maxIdleConns)
	writerConn.SetConnMaxLifetime(connMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), connectionSetupTimeout)
	defer cancel()
	if err := writerConn.PingContext(ctx); err != nil {
		_ = writerConn.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	// Separate pool for readers to preserve existing architecture.
	readerPool, err := sql.Open("pgx", dsn)
	if err != nil {
		_ = writerConn.Close()
		return nil, fmt.Errorf("open postgres reader pool: %w", err)
	}
	readerPool.SetMaxOpenConns(maxOpenConns)
	readerPool.SetMaxIdleConns(maxIdleConns)
	readerPool.SetConnMaxLifetime(connMaxLifetime)
	if err := readerPool.PingContext(ctx); err != nil {
		_ = writerConn.Close()
		_ = readerPool.Close()
		return nil, fmt.Errorf("ping postgres reader pool: %w", err)
	}

	writerStmtsCache := newStmtCache()
	readerStmtsCache := newStmtCache()

	db := &DB{
		writerConn:      writerConn,
		readerPool:      readerPool,
		writerStmts:     writerStmtsCache,
		readerStmts:     readerStmtsCache,
		dialect:         DialectPostgres,
		serializeWrites: false,
	}

	if err := db.migratePostgres(); err != nil {
		_ = writerConn.Close()
		_ = readerPool.Close()
		return nil, fmt.Errorf("run postgres migrations: %w", err)
	}

	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	db.cleanupCancel = cleanupCancel
	go db.stringPoolCleanupLoop(cleanupCtx)

	return db, nil
}

func newStmtCache() *ttlcache.Cache[string, *sql.Stmt] {
	opts := ttlcache.Options[string, *sql.Stmt]{}.SetDefaultTTL(5 * time.Minute).
		SetDeallocationFunc(func(_ string, s *sql.Stmt, _ ttlcache.DeallocationReason) {
			if s != nil {
				_ = s.Close()
			}
		})
	return ttlcache.New(opts)
}

func (db *DB) migratePostgres() error {
	ctx := context.Background()

	// Prevent concurrent migrators on the same DB.
	tx, err := db.writerConn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(922337203685477000)"); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS migrations (
			id BIGSERIAL PRIMARY KEY,
			filename TEXT NOT NULL UNIQUE,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	entries, err := postgresMigrationsFS.ReadDir("postgres_migrations")
	if err != nil {
		// No postgres migrations embedded: keep existing schema as-is.
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit empty migration tx: %w", err)
		}
		return nil
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	for _, filename := range files {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrations WHERE filename = $1", filename).Scan(&count); err != nil {
			return fmt.Errorf("check migration %s: %w", filename, err)
		}
		if count > 0 {
			continue
		}

		content, err := postgresMigrationsFS.ReadFile("postgres_migrations/" + filename)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", filename, err)
		}
		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			return fmt.Errorf("execute migration %s: %w", filename, err)
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO migrations (filename) VALUES ($1)", filename); err != nil {
			return fmt.Errorf("record migration %s: %w", filename, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit postgres migrations: %w", err)
	}
	return nil
}
