// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Repository handles database operations for torrent file caching
type Repository struct {
	db dbinterface.Querier
}

// NewRepository creates a new files repository
func NewRepository(db dbinterface.Querier) *Repository {
	return &Repository{db: db}
}

// GetFiles retrieves all cached files for a torrent
func (r *Repository) GetFiles(ctx context.Context, instanceID int, hash string) ([]CachedFile, error) {
	return r.getFiles(ctx, r.db, instanceID, hash)
}

// GetFilesTx retrieves all cached files for a torrent within a transaction
func (r *Repository) GetFilesTx(ctx context.Context, tx dbinterface.TxQuerier, instanceID int, hash string) ([]CachedFile, error) {
	return r.getFiles(ctx, tx, instanceID, hash)
}

// getFiles is the internal implementation that works with any querier (db or tx)
func (r *Repository) getFiles(ctx context.Context, q querier, instanceID int, hash string) ([]CachedFile, error) {
	query := `
		SELECT id, instance_id, torrent_hash, file_index, name, size, progress, 
		       priority, is_seed, piece_range_start, piece_range_end, availability, cached_at
		FROM torrent_files_cache_view
		WHERE instance_id = ? AND torrent_hash = ?
		ORDER BY file_index ASC
	`

	rows, err := q.QueryContext(ctx, query, instanceID, hash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []CachedFile
	for rows.Next() {
		var f CachedFile
		var isSeed sql.NullBool
		err := rows.Scan(
			&f.ID,
			&f.InstanceID,
			&f.TorrentHash,
			&f.FileIndex,
			&f.Name,
			&f.Size,
			&f.Progress,
			&f.Priority,
			&isSeed,
			&f.PieceRangeStart,
			&f.PieceRangeEnd,
			&f.Availability,
			&f.CachedAt,
		)
		if err != nil {
			return nil, err
		}

		if isSeed.Valid {
			f.IsSeed = &isSeed.Bool
		}

		files = append(files, f)
	}

	return files, rows.Err()
}

// querier interface for methods that accept db or tx
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// UpsertFiles inserts or updates cached file information.
//
// CONCURRENCY MODEL: This function uses eventual consistency with last-writer-wins semantics.
// If two goroutines cache the same torrent concurrently:
// - Each file row UPSERT is atomic at the SQLite level
// - The last write wins for each individual file
// - Progress/availability values may briefly be inconsistent across files
// - This is acceptable because:
//  1. Cache freshness checks (5min TTL for active torrents) limit staleness
//  2. Complete torrents (100% progress) have stable values
//  3. UI shows slightly stale data briefly, then refreshes naturally
//  4. Strict consistency would require distributed locks with significant overhead
//
// Alternative approaches considered but rejected:
// - Optimistic locking with version numbers: adds complexity, breaks on every concurrent write
// - Exclusive locks during cache write: defeats purpose of caching, creates bottleneck
//
// ATOMICITY: All files are upserted within a single transaction to ensure all-or-nothing semantics.
// If any file insert fails, the entire operation is rolled back to prevent partial cache states.
func (r *Repository) UpsertFiles(ctx context.Context, files []CachedFile) error {
	if len(files) == 0 {
		return nil
	}

	hash := files[0].TorrentHash

	// Begin transaction for atomic upsert of all files
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the torrent hash first
	hashID, err := dbinterface.InternString(ctx, tx, hash)
	if err != nil {
		return fmt.Errorf("failed to intern torrent_hash: %w", err)
	}

	for _, f := range files {
		// Intern the file name
		nameID, err := dbinterface.InternString(ctx, tx, f.Name)
		if err != nil {
			return fmt.Errorf("failed to intern name: %w", err)
		}

		var isSeed interface{}
		if f.IsSeed != nil {
			isSeed = *f.IsSeed
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO torrent_files_cache 
			(instance_id, torrent_hash_id, file_index, name_id, size, progress, priority, 
			 is_seed, piece_range_start, piece_range_end, availability, cached_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(instance_id, torrent_hash_id, file_index) DO UPDATE SET
				name_id = excluded.name_id,
				size = excluded.size,
				progress = excluded.progress,
				priority = excluded.priority,
				is_seed = excluded.is_seed,
				piece_range_start = excluded.piece_range_start,
				piece_range_end = excluded.piece_range_end,
				availability = excluded.availability,
				cached_at = excluded.cached_at
		`,
			f.InstanceID,
			hashID,
			f.FileIndex,
			nameID,
			f.Size,
			f.Progress,
			f.Priority,
			isSeed,
			f.PieceRangeStart,
			f.PieceRangeEnd,
			f.Availability,
			time.Now(),
		)
		if err != nil {
			return fmt.Errorf("failed to insert file %d: %w", f.FileIndex, err)
		}
	}

	// Commit transaction to make all changes atomic
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// DeleteFiles removes all cached files for a torrent.
// Returns nil if successful or if no cache existed for the given torrent.
// To distinguish between "deleted" vs "nothing to delete", check the logs or
// use GetFiles before calling this method.
func (r *Repository) DeleteFiles(ctx context.Context, instanceID int, hash string) error {
	// Start a transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the hash to get its ID
	hashID, err := dbinterface.InternString(ctx, tx, hash)
	if err != nil {
		return fmt.Errorf("failed to intern torrent_hash: %w", err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM torrent_files_cache WHERE instance_id = ? AND torrent_hash_id = ?`, instanceID, hashID)
	if err != nil {
		return fmt.Errorf("failed to delete cached files: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Log how many rows were deleted for observability
	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected > 0 {
		log.Debug().Int("instanceID", instanceID).Str("hash", hash).Int64("files", rowsAffected).
			Msg("Deleted cached files")
	}

	return nil
}

// GetSyncInfo retrieves sync metadata for a torrent
func (r *Repository) GetSyncInfo(ctx context.Context, instanceID int, hash string) (*SyncInfo, error) {
	return r.getSyncInfo(ctx, r.db, instanceID, hash)
}

// GetSyncInfoTx retrieves sync metadata for a torrent within a transaction
func (r *Repository) GetSyncInfoTx(ctx context.Context, tx dbinterface.TxQuerier, instanceID int, hash string) (*SyncInfo, error) {
	return r.getSyncInfo(ctx, tx, instanceID, hash)
}

// getSyncInfo is the internal implementation that works with any querier (db or tx)
func (r *Repository) getSyncInfo(ctx context.Context, q querier, instanceID int, hash string) (*SyncInfo, error) {
	query := `
		SELECT instance_id, torrent_hash, last_synced_at, torrent_progress, file_count
		FROM torrent_files_sync_view
		WHERE instance_id = ? AND torrent_hash = ?
	`

	var info SyncInfo
	err := q.QueryRowContext(ctx, query, instanceID, hash).Scan(
		&info.InstanceID,
		&info.TorrentHash,
		&info.LastSyncedAt,
		&info.TorrentProgress,
		&info.FileCount,
	)

	if err != nil {
		return nil, err
	}

	return &info, nil
}

// UpsertSyncInfo inserts or updates sync metadata
func (r *Repository) UpsertSyncInfo(ctx context.Context, info SyncInfo) error {
	// Start a transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the torrent hash
	hashID, err := dbinterface.InternString(ctx, tx, info.TorrentHash)
	if err != nil {
		return fmt.Errorf("failed to intern torrent_hash: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO torrent_files_sync 
		(instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, torrent_hash_id) DO UPDATE SET
			last_synced_at = excluded.last_synced_at,
			torrent_progress = excluded.torrent_progress,
			file_count = excluded.file_count
	`,
		info.InstanceID,
		hashID,
		info.LastSyncedAt,
		info.TorrentProgress,
		info.FileCount,
	)

	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteSyncInfo removes sync metadata for a torrent
func (r *Repository) DeleteSyncInfo(ctx context.Context, instanceID int, hash string) error {
	// Start a transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Intern the hash to get its ID
	hashID, err := dbinterface.InternString(ctx, tx, hash)
	if err != nil {
		return fmt.Errorf("failed to intern torrent_hash: %w", err)
	}

	_, err = tx.ExecContext(ctx, `DELETE FROM torrent_files_sync WHERE instance_id = ? AND torrent_hash_id = ?`, instanceID, hashID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteOldCache removes cache entries older than the specified time
func (r *Repository) DeleteOldCache(ctx context.Context, olderThan time.Time) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// First delete from files cache
	filesQuery := `
		DELETE FROM torrent_files_cache 
		WHERE (instance_id, torrent_hash_id) IN (
			SELECT instance_id, torrent_hash_id 
			FROM torrent_files_sync 
			WHERE last_synced_at < ?
		)
	`
	result, err := tx.ExecContext(ctx, filesQuery, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old files: %w", err)
	}

	// Then delete from sync info
	syncQuery := `DELETE FROM torrent_files_sync WHERE last_synced_at < ?`
	_, err = tx.ExecContext(ctx, syncQuery, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old sync info: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return int(rowsAffected), nil
}

// GetCacheStats returns statistics about the cache for an instance
func (r *Repository) GetCacheStats(ctx context.Context, instanceID int) (*CacheStats, error) {
	query := `
		SELECT 
			COUNT(DISTINCT torrent_hash) as cached_torrents,
			COUNT(*) as total_files,
			MIN(julianday('now') - julianday(last_synced_at)) * 86400 as oldest_seconds,
			MAX(julianday('now') - julianday(last_synced_at)) * 86400 as newest_seconds,
			AVG(julianday('now') - julianday(last_synced_at)) * 86400 as avg_seconds
		FROM torrent_files_sync_view
		WHERE instance_id = ?
	`

	var stats CacheStats
	var oldestSecs, newestSecs, avgSecs sql.NullFloat64

	err := r.db.QueryRowContext(ctx, query, instanceID).Scan(
		&stats.CachedTorrents,
		&stats.TotalFiles,
		&oldestSecs,
		&newestSecs,
		&avgSecs,
	)

	if err != nil {
		return nil, err
	}

	if oldestSecs.Valid {
		dur := time.Duration(oldestSecs.Float64 * float64(time.Second))
		stats.OldestCacheAge = &dur
	}

	if newestSecs.Valid {
		dur := time.Duration(newestSecs.Float64 * float64(time.Second))
		stats.NewestCacheAge = &dur
	}

	if avgSecs.Valid {
		dur := time.Duration(avgSecs.Float64 * float64(time.Second))
		stats.AverageCacheAge = &dur
	}

	return &stats, nil
}
