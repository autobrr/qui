// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var (
	// validHashRegex matches SHA-1 (40 hex) or SHA-256 (64 hex) hashes
	validHashRegex = regexp.MustCompile(`^[a-fA-F0-9]{40}$|^[a-fA-F0-9]{64}$`)
)

// validateHash checks if a hash is a valid torrent hash format
func validateHash(hash string) bool {
	return validHashRegex.MatchString(hash)
}

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
func (r *Repository) GetFilesTx(ctx context.Context, tx *sql.Tx, instanceID int, hash string) ([]CachedFile, error) {
	return r.getFiles(ctx, tx, instanceID, hash)
}

// getFiles is the internal implementation that works with any querier (db or tx)
func (r *Repository) getFiles(ctx context.Context, q querier, instanceID int, hash string) ([]CachedFile, error) {
	// Validate hash format before database operations
	if !validateHash(hash) {
		return nil, fmt.Errorf("invalid torrent hash format: %s", hash)
	}

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

// UpsertFiles inserts or updates cached file information
func (r *Repository) UpsertFiles(ctx context.Context, files []CachedFile) error {
	if len(files) == 0 {
		return nil
	}

	hash := files[0].TorrentHash

	// Validate hash format before database operations
	if !validateHash(hash) {
		return fmt.Errorf("invalid torrent hash format: %s", hash)
	}

	// Get or create string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, hash, nil)
	if err != nil {
		return fmt.Errorf("failed to intern torrent hash: %w", err)
	}

	// Upsert all files
	insertQuery := `
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
	`

	for _, f := range files {
		// Get or create string ID for file name
		nameID, err := r.db.GetOrCreateStringID(ctx, f.Name, nil)
		if err != nil {
			return fmt.Errorf("failed to intern file name: %w", err)
		}

		var isSeed interface{}
		if f.IsSeed != nil {
			isSeed = *f.IsSeed
		}

		_, err = r.db.ExecContext(ctx, insertQuery,
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

	return nil
}

// DeleteFiles removes all cached files for a torrent
func (r *Repository) DeleteFiles(ctx context.Context, instanceID int, hash string) error {
	// Validate hash format before database operations
	if !validateHash(hash) {
		return fmt.Errorf("invalid torrent hash format: %s", hash)
	}

	// Get string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, hash, nil)
	if err != nil {
		// If hash doesn't exist in pool, no files exist
		return nil
	}

	query := `DELETE FROM torrent_files_cache WHERE instance_id = ? AND torrent_hash_id = ?`
	_, err = r.db.ExecContext(ctx, query, instanceID, hashID)
	return err
}

// GetSyncInfo retrieves sync metadata for a torrent
func (r *Repository) GetSyncInfo(ctx context.Context, instanceID int, hash string) (*SyncInfo, error) {
	return r.getSyncInfo(ctx, r.db, instanceID, hash)
}

// GetSyncInfoTx retrieves sync metadata for a torrent within a transaction
func (r *Repository) GetSyncInfoTx(ctx context.Context, tx *sql.Tx, instanceID int, hash string) (*SyncInfo, error) {
	return r.getSyncInfo(ctx, tx, instanceID, hash)
}

// getSyncInfo is the internal implementation that works with any querier (db or tx)
func (r *Repository) getSyncInfo(ctx context.Context, q querier, instanceID int, hash string) (*SyncInfo, error) {
	// Validate hash format before database operations
	if !validateHash(hash) {
		return nil, fmt.Errorf("invalid torrent hash format: %s", hash)
	}

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
	// Validate hash format before database operations
	if !validateHash(info.TorrentHash) {
		return fmt.Errorf("invalid torrent hash format: %s", info.TorrentHash)
	}

	// Get or create string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, info.TorrentHash, nil)
	if err != nil {
		return fmt.Errorf("failed to intern torrent hash: %w", err)
	}

	query := `
		INSERT INTO torrent_files_sync 
		(instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, torrent_hash_id) DO UPDATE SET
			last_synced_at = excluded.last_synced_at,
			torrent_progress = excluded.torrent_progress,
			file_count = excluded.file_count
	`

	_, err = r.db.ExecContext(ctx, query,
		info.InstanceID,
		hashID,
		info.LastSyncedAt,
		info.TorrentProgress,
		info.FileCount,
	)

	return err
}

// DeleteSyncInfo removes sync metadata for a torrent
func (r *Repository) DeleteSyncInfo(ctx context.Context, instanceID int, hash string) error {
	// Validate hash format before database operations
	if !validateHash(hash) {
		return fmt.Errorf("invalid torrent hash format: %s", hash)
	}

	// Get string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, hash, nil)
	if err != nil {
		// If hash doesn't exist in pool, no sync info exists
		return nil
	}

	query := `DELETE FROM torrent_files_sync WHERE instance_id = ? AND torrent_hash_id = ?`
	_, err = r.db.ExecContext(ctx, query, instanceID, hashID)
	return err
}

// DeleteOldCache removes cache entries older than the specified time
func (r *Repository) DeleteOldCache(ctx context.Context, olderThan time.Time) (int, error) {
	// First delete from files cache
	filesQuery := `
		DELETE FROM torrent_files_cache 
		WHERE (instance_id, torrent_hash_id) IN (
			SELECT instance_id, torrent_hash_id 
			FROM torrent_files_sync 
			WHERE last_synced_at < ?
		)
	`
	result, err := r.db.ExecContext(ctx, filesQuery, olderThan)
	if err != nil {
		return 0, err
	}

	// Then delete from sync info
	syncQuery := `DELETE FROM torrent_files_sync WHERE last_synced_at < ?`
	_, err = r.db.ExecContext(ctx, syncQuery, olderThan)
	if err != nil {
		return 0, err
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
