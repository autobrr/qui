// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Repository handles database operations for torrent file caching
type Repository struct {
	db dbinterface.DBWithStringInterning
}

// NewRepository creates a new files repository
func NewRepository(db dbinterface.DBWithStringInterning) *Repository {
	return &Repository{db: db}
}

// GetFiles retrieves all cached files for a torrent
func (r *Repository) GetFiles(ctx context.Context, instanceID int, hash string) ([]CachedFile, error) {
	query := `
		SELECT id, instance_id, torrent_hash, file_index, name, size, progress, 
		       priority, is_seed, piece_range_start, piece_range_end, availability, cached_at
		FROM torrent_files_cache_view
		WHERE instance_id = ? AND torrent_hash = ?
		ORDER BY file_index ASC
	`

	rows, err := r.db.QueryContext(ctx, query, instanceID, hash)
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

// UpsertFiles inserts or updates cached file information
func (r *Repository) UpsertFiles(ctx context.Context, files []CachedFile) error {
	if len(files) == 0 {
		return nil
	}

	// First, delete existing files for this torrent
	instanceID := files[0].InstanceID
	hash := files[0].TorrentHash

	// Get or create string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, hash)
	if err != nil {
		return fmt.Errorf("failed to intern torrent hash: %w", err)
	}

	deleteQuery := `DELETE FROM torrent_files_cache WHERE instance_id = ? AND torrent_hash_id = ?`
	if _, err := r.db.ExecContext(ctx, deleteQuery, instanceID, hashID); err != nil {
		return fmt.Errorf("failed to delete existing files: %w", err)
	}

	// Insert all files
	insertQuery := `
		INSERT INTO torrent_files_cache 
		(instance_id, torrent_hash_id, file_index, name_id, size, progress, priority, 
		 is_seed, piece_range_start, piece_range_end, availability, cached_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	for _, f := range files {
		// Get or create string ID for file name
		nameID, err := r.db.GetOrCreateStringID(ctx, f.Name)
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
	// Get string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, hash)
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
	query := `
		SELECT instance_id, torrent_hash, last_synced_at, torrent_progress, file_count
		FROM torrent_files_sync_view
		WHERE instance_id = ? AND torrent_hash = ?
	`

	var info SyncInfo
	err := r.db.QueryRowContext(ctx, query, instanceID, hash).Scan(
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
	// Get or create string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, info.TorrentHash)
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
	// Get string ID for torrent hash
	hashID, err := r.db.GetOrCreateStringID(ctx, hash)
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
		WHERE (instance_id, torrent_hash) IN (
			SELECT instance_id, torrent_hash 
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
