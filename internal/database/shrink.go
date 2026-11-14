// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"
	"modernc.org/sqlite"
	sqlitelib "modernc.org/sqlite/lib"
)

// ErrShrinkUnsupported is the answer of a shrink call on Postgres.
var ErrShrinkUnsupported = errors.New("only a SQLite database file can shrink")

var errCheckpointBusy = errors.New("another program held the database, so the file did not get smaller")

// The "shrink pending" mark lives in the user_version field of the SQLite file
// header. qui does not use that field for anything else.
const shrinkPendingMark = 1

// ShrinkFailure names the kind of cause of a failed shrink.
type ShrinkFailure string

const (
	ShrinkDiskFull ShrinkFailure = "disk_full"
	ShrinkLocked   ShrinkFailure = "locked"
	ShrinkUnknown  ShrinkFailure = "unknown"
)

// ShrinkResult is the outcome of the shrink at startup. Err is nil when the
// shrink succeeded.
type ShrinkResult struct {
	BeforeBytes int64
	AfterBytes  int64
	Err         error
	Failure     ShrinkFailure
}

// runVacuum is a variable so that a test can make the shrink fail.
var runVacuum = func(ctx context.Context, conn *sql.DB) error {
	_, err := conn.ExecContext(ctx, "VACUUM")
	return err
}

// SQLitePath returns the path of the SQLite database file, or "" on Postgres.
func (db *DB) SQLitePath() string {
	return db.sqlitePath
}

// SQLiteFileDir returns the directory that holds the SQLite database file,
// after symlinks, so that a shrink uses the disk of the data.
func (db *DB) SQLiteFileDir() (string, error) {
	resolved, err := filepath.EvalSymlinks(db.sqlitePath)
	if err != nil {
		return "", err
	}
	return filepath.Dir(resolved), nil
}

// RequestShrink stores the "shrink pending" mark. The next RunPendingShrink,
// normally at the next start of qui, shrinks the file.
func (db *DB) RequestShrink(ctx context.Context) error {
	return db.setShrinkMark(ctx, shrinkPendingMark)
}

// ShrinkPending reports whether the "shrink pending" mark is stored.
func (db *DB) ShrinkPending(ctx context.Context) (bool, error) {
	if db.dialect != DialectSQLite {
		return false, nil
	}
	var mark int
	if err := db.readerPool.QueryRowContext(ctx, "PRAGMA user_version").Scan(&mark); err != nil {
		return false, fmt.Errorf("read shrink mark: %w", err)
	}
	return mark == shrinkPendingMark, nil
}

// RunPendingShrink shrinks the SQLite file when the "shrink pending" mark is
// stored, and clears the mark. Call it at startup, before anything else uses
// the database. It reports false when no shrink ran. A failed shrink leaves the
// database as it was, and the mark stays cleared, so qui does not try again on
// its own.
func (db *DB) RunPendingShrink(ctx context.Context) (ShrinkResult, bool) {
	pending, err := db.ShrinkPending(ctx)
	if err != nil {
		log.Error().Err(err).Msg("database shrink: cannot read the mark")
		return ShrinkResult{}, false
	}
	if !pending {
		return ShrinkResult{}, false
	}

	// Clear the mark before the shrink: a shrink that ends the process, for
	// example on a full disk, must not run again at each start.
	if err := db.setShrinkMark(ctx, 0); err != nil {
		log.Error().Err(err).Msg("database shrink: cannot clear the mark, so qui runs no shrink")
		return failedShrink(0, err), true
	}

	result := db.shrink(ctx)
	if result.Err != nil {
		log.Error().Err(result.Err).Str("failure", string(result.Failure)).Msg("database shrink failed")
	} else {
		log.Info().Int64("beforeBytes", result.BeforeBytes).Int64("afterBytes", result.AfterBytes).Msg("database shrink done")
	}
	return result, true
}

func (db *DB) shrink(ctx context.Context) ShrinkResult {
	if err := db.lockWriter(ctx); err != nil {
		return failedShrink(0, err)
	}
	defer db.unlockWriter()

	before, err := fileSize(db.sqlitePath)
	if err != nil {
		return failedShrink(0, err)
	}
	// Windows cannot make a file smaller while another connection maps it,
	// and an idle reader keeps its map of the file.
	db.readerPool.SetMaxIdleConns(0)
	defer db.readerPool.SetMaxIdleConns(sqliteReaderPoolSize())
	// VACUUM builds its temporary copy in the temp directory of the system.
	// Build it next to the database, so that the free space check of one disk
	// holds. The setting is global to the process, and at startup nothing else
	// uses SQLite.
	dataDir, err := db.SQLiteFileDir()
	if err != nil {
		return failedShrink(before, err)
	}
	tempDir := strings.ReplaceAll(dataDir, "'", "''")
	if _, err := db.writerConn.ExecContext(ctx, "PRAGMA temp_store_directory = '"+tempDir+"'"); err != nil {
		return failedShrink(before, fmt.Errorf("set the temp directory of the shrink: %w", err))
	}
	defer func() {
		if _, err := db.writerConn.ExecContext(context.WithoutCancel(ctx), "PRAGMA temp_store_directory = ''"); err != nil {
			log.Warn().Err(err).Msg("database shrink: cannot reset the temp directory")
		}
	}()
	if err := runVacuum(ctx, db.writerConn); err != nil {
		// Give the disk back the pages that the failed VACUUM wrote to the WAL.
		_, _ = db.checkpoint(context.WithoutCancel(ctx))
		return failedShrink(before, err)
	}
	// In WAL mode, the file gets smaller when the checkpoint moves the new
	// pages back into it.
	busy, err := db.checkpoint(ctx)
	if err != nil {
		return failedShrink(before, fmt.Errorf("checkpoint after vacuum: %w", err))
	}
	if busy {
		return failedShrink(before, errCheckpointBusy)
	}
	after, err := fileSize(db.sqlitePath)
	if err != nil {
		return failedShrink(before, err)
	}
	return ShrinkResult{BeforeBytes: before, AfterBytes: after}
}

// checkpoint moves the WAL into the database file and empties the WAL. It
// reports busy when another connection kept it from the end.
func (db *DB) checkpoint(ctx context.Context) (busy bool, err error) {
	var blocked, walFrames, moved int
	if err := db.writerConn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&blocked, &walFrames, &moved); err != nil {
		return false, err
	}
	return blocked != 0, nil
}

func (db *DB) setShrinkMark(ctx context.Context, mark int) error {
	if db.dialect != DialectSQLite {
		return ErrShrinkUnsupported
	}
	if err := db.lockWriter(ctx); err != nil {
		return err
	}
	defer db.unlockWriter()
	// PRAGMA takes no bound value.
	if _, err := db.writerConn.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", mark)); err != nil {
		return fmt.Errorf("store shrink mark: %w", err)
	}
	return nil
}

func failedShrink(before int64, err error) ShrinkResult {
	failure := ShrinkUnknown
	if errors.Is(err, errCheckpointBusy) {
		failure = ShrinkLocked
	}
	if sqlErr, ok := errors.AsType[*sqlite.Error](err); ok {
		switch sqlErr.Code() & 0xff {
		case sqlitelib.SQLITE_FULL:
			failure = ShrinkDiskFull
		case sqlitelib.SQLITE_BUSY, sqlitelib.SQLITE_LOCKED:
			failure = ShrinkLocked
		}
	}
	return ShrinkResult{BeforeBytes: before, Err: err, Failure: failure}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}
