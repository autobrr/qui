// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

// Repository handles database operations for torrent file caching
type Repository struct {
	db dbinterface.Querier
}

// Use a conservative batch size to stay under SQLite parameter limits (900 default).
const maxBatchItems = 800

// Use a conservative batch size for file inserts to stay under SQLite parameter limits (999 default).
const fileBatchSize = 80

// fileRow represents a single row to insert into torrent_files_cache
type fileRow struct {
	instanceID      int
	hashID          int64
	fileIndex       int
	nameID          int64
	size            int64
	progress        float64
	priority        int
	isSeed          interface{}
	pieceRangeStart int64
	pieceRangeEnd   int64
	availability    float64
}

// NewRepository creates a new files repository
func NewRepository(db dbinterface.Querier) *Repository {
	return &Repository{db: db}
}

// GetFiles retrieves all cached files for a torrent
func (r *Repository) GetFiles(ctx context.Context, instanceID int, hash string) ([]CachedFile, error) {
	return r.getFiles(ctx, r.db, instanceID, hash)
}

// GetFilesBatch retrieves cached files for multiple torrents at once.
func (r *Repository) GetFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string][]CachedFile, error) {
	cleaned := dedupeHashes(hashes)
	if len(cleaned) == 0 {
		return map[string][]CachedFile{}, nil
	}

	results := make(map[string][]CachedFile, len(cleaned))
	for _, batch := range chunkHashes(cleaned, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, h := range batch {
			args = append(args, h)
		}

		query := fmt.Sprintf(`
			SELECT id, instance_id, torrent_hash, file_index, name, size, progress,
			       priority, is_seed, piece_range_start, piece_range_end, availability, cached_at
			FROM torrent_files_cache_view
			WHERE instance_id = ? AND torrent_hash IN (%s)
			ORDER BY torrent_hash, file_index ASC
		`, buildPlaceholders(len(batch)))

		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var (
				f      CachedFile
				isSeed sql.NullBool
			)
			if err := rows.Scan(
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
			); err != nil {
				rows.Close()
				return nil, err
			}

			if isSeed.Valid {
				f.IsSeed = &isSeed.Bool
			}

			results[f.TorrentHash] = append(results[f.TorrentHash], f)
		}

		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return results, nil
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

	// Group files by torrent hash
	filesByHash := make(map[string][]CachedFile)
	for _, f := range files {
		filesByHash[f.TorrentHash] = append(filesByHash[f.TorrentHash], f)
	}

	// Collect all unique strings to intern (all hashes + all file names).
	// Preserve hash insertion order explicitly so we can map IDs back correctly.
	hashOrder := make([]string, 0, len(filesByHash))
	for hash := range filesByHash {
		hashOrder = append(hashOrder, hash)
	}
	allStrings := make([]string, 0, len(hashOrder)+len(files))
	allStrings = append(allStrings, hashOrder...)
	for _, f := range files {
		allStrings = append(allStrings, f.Name)
	}

	// Begin transaction for atomic upsert of all files
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := dbinterface.DeferForeignKeyChecks(ctx, tx); err != nil {
		return fmt.Errorf("failed to defer foreign keys: %w", err)
	}

	// Batch intern all strings
	allIDs, err := dbinterface.InternStrings(ctx, tx, allStrings...)
	if err != nil {
		return fmt.Errorf("failed to intern strings: %w", err)
	}

	valueToID := make(map[string]int64, len(allStrings))
	for i, value := range allStrings {
		valueToID[value] = allIDs[i]
	}

	hashIDMap := make(map[string]int64, len(hashOrder))
	for _, hash := range hashOrder {
		hashIDMap[hash] = valueToID[hash]
	}

	// Collect all file rows to batch insert
	var allRows []fileRow
	for hash, fileGroup := range filesByHash {
		hashID, ok := hashIDMap[hash]
		if !ok {
			return fmt.Errorf("missing interned ID for hash %s", hash)
		}
		for _, f := range fileGroup {
			nameID, ok := valueToID[f.Name]
			if !ok {
				return fmt.Errorf("missing interned ID for file %s", f.Name)
			}

			var isSeed interface{}
			if f.IsSeed != nil {
				isSeed = *f.IsSeed
			}

			allRows = append(allRows, fileRow{
				instanceID:      f.InstanceID,
				hashID:          hashID,
				fileIndex:       f.FileIndex,
				nameID:          nameID,
				size:            f.Size,
				progress:        f.Progress,
				priority:        f.Priority,
				isSeed:          isSeed,
				pieceRangeStart: f.PieceRangeStart,
				pieceRangeEnd:   f.PieceRangeEnd,
				availability:    f.Availability,
			})
		}
	}

	// Pre-build the full query for full batches
	queryTemplate := `
			INSERT INTO torrent_files_cache
			(instance_id, torrent_hash_id, file_index, name_id, size, progress, priority,
			 is_seed, piece_range_start, piece_range_end, availability, cached_at)
			VALUES %s
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
	fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 12, fileBatchSize)
	t := time.Now()

	// Pre-allocate args slice to reuse across batches
	args := make([]interface{}, 0, fileBatchSize*12)

	// Batch insert files
	for i := 0; i < len(allRows); i += fileBatchSize {
		end := i + fileBatchSize
		if end > len(allRows) {
			end = len(allRows)
		}
		batch := allRows[i:end]

		// Reset args for this batch
		args = args[:0]
		var query string
		if len(batch) == fileBatchSize {
			query = fullBatchQuery
		} else {
			// Build query for partial final batch
			query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 12, len(batch))
		}

		for _, row := range batch {
			args = append(args,
				row.instanceID,
				row.hashID,
				row.fileIndex,
				row.nameID,
				row.size,
				row.progress,
				row.priority,
				row.isSeed,
				row.pieceRangeStart,
				row.pieceRangeEnd,
				row.availability,
				t,
			)
		}

		_, err = tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to batch insert files: %w", err)
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

	// Get the hash ID without creating it if it doesn't exist
	hashIDs, err := dbinterface.GetStringID(ctx, tx, hash)
	if err != nil {
		return fmt.Errorf("failed to get torrent_hash ID: %w", err)
	}

	// If the hash doesn't exist in the string pool, there's nothing to delete
	if len(hashIDs) == 0 || !hashIDs[0].Valid {
		return tx.Commit()
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM torrent_files_cache WHERE instance_id = ? AND torrent_hash_id = ?`, instanceID, hashIDs[0].Int64)
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

// GetSyncInfoBatch retrieves sync metadata for multiple torrents in a single query.
func (r *Repository) GetSyncInfoBatch(ctx context.Context, instanceID int, hashes []string) (map[string]*SyncInfo, error) {
	cleaned := dedupeHashes(hashes)
	if len(cleaned) == 0 {
		return map[string]*SyncInfo{}, nil
	}

	results := make(map[string]*SyncInfo, len(cleaned))
	for _, batch := range chunkHashes(cleaned, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, h := range batch {
			args = append(args, h)
		}

		query := fmt.Sprintf(`
			SELECT instance_id, torrent_hash, last_synced_at, torrent_progress, file_count
			FROM torrent_files_sync_view
			WHERE instance_id = ? AND torrent_hash IN (%s)
		`, buildPlaceholders(len(batch)))

		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var info SyncInfo
			if err := rows.Scan(
				&info.InstanceID,
				&info.TorrentHash,
				&info.LastSyncedAt,
				&info.TorrentProgress,
				&info.FileCount,
			); err != nil {
				rows.Close()
				return nil, err
			}

			results[info.TorrentHash] = &info
		}

		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return results, nil
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
	ids, err := dbinterface.InternStrings(ctx, tx, info.TorrentHash)
	if err != nil {
		return fmt.Errorf("failed to intern torrent_hash: %w", err)
	}
	hashID := ids[0]

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

// UpsertSyncInfoBatch inserts or updates sync metadata for multiple torrents
func (r *Repository) UpsertSyncInfoBatch(ctx context.Context, infos []SyncInfo) error {
	if len(infos) == 0 {
		return nil
	}

	// Start a transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("UpsertSyncInfoBatch: failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Collect all hashes to intern
	hashes := make([]string, len(infos))
	for i, info := range infos {
		hashes[i] = info.TorrentHash
	}

	// Batch intern all hashes
	hashIDs, err := dbinterface.InternStrings(ctx, tx, hashes...)
	if err != nil {
		return fmt.Errorf("UpsertSyncInfoBatch: failed to intern torrent_hashes: %w", err)
	}

	// Batch size for sync info inserts (5 placeholders per row, keep under SQLite's 999 limit)
	const syncBatchSize = 150

	// Build bulk upsert query template
	queryTemplate := `
		INSERT INTO torrent_files_sync
		(instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count)
		VALUES %s
		ON CONFLICT(instance_id, torrent_hash_id) DO UPDATE SET
			last_synced_at = excluded.last_synced_at,
			torrent_progress = excluded.torrent_progress,
			file_count = excluded.file_count
	`

	// Pre-build the full query for full batches
	fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 5, syncBatchSize)

	// Pre-allocate args slice to reuse across batches
	args := make([]interface{}, 0, syncBatchSize*5)

	// Batch insert sync infos
	for i := 0; i < len(infos); i += syncBatchSize {
		end := i + syncBatchSize
		if end > len(infos) {
			end = len(infos)
		}
		batch := infos[i:end]

		// Reset args for this batch
		args = args[:0]
		var query string
		if len(batch) == syncBatchSize {
			query = fullBatchQuery
		} else {
			// Build query for partial final batch
			query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 5, len(batch))
		}

		for j, info := range batch {
			hashID := hashIDs[i+j]
			args = append(args,
				info.InstanceID,
				hashID,
				info.LastSyncedAt,
				info.TorrentProgress,
				info.FileCount,
			)
		}

		_, err = tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("UpsertSyncInfoBatch: failed to upsert sync info batch: %w", err)
		}
	}

	return tx.Commit()
}

// DeleteTorrentCache removes all cache data for multiple torrents (both files and sync info)
func (r *Repository) DeleteTorrentCache(ctx context.Context, instanceID int, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}

	hashes = dedupeHashes(hashes)
	if len(hashes) == 0 {
		return nil
	}

	// Start a transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("DeleteTorrentCache: failed to begin transaction: %w", err)
	}
	defer func() {
		if rollErr := tx.Rollback(); rollErr != nil && !errors.Is(rollErr, sql.ErrTxDone) && err == nil {
			err = fmt.Errorf("DeleteTorrentCache: rollback failed: %w", rollErr)
		}
	}()

	// Get hash IDs for all hashes
	hashIDs, err := dbinterface.GetStringID(ctx, tx, hashes...)
	if err != nil {
		return fmt.Errorf("DeleteTorrentCache: failed to get torrent_hash IDs: %w", err)
	}

	// Filter out invalid hashes
	validHashIDs := make([]int64, 0, len(hashIDs))
	for _, id := range hashIDs {
		if id.Valid {
			validHashIDs = append(validHashIDs, id.Int64)
		}
	}

	if len(validHashIDs) == 0 {
		// No valid hashes to delete
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("DeleteTorrentCache: commit failed when no valid hashes: %w", commitErr)
		}
		return nil
	}

	// Delete files for all valid hashes
	for _, batch := range chunkInts(validHashIDs, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, id := range batch {
			args = append(args, id)
		}

		query := fmt.Sprintf(`DELETE FROM torrent_files_cache WHERE instance_id = ? AND torrent_hash_id IN (%s)`, buildPlaceholders(len(batch)))
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("DeleteTorrentCache: failed to delete cached files: %w", err)
		}
	}

	// Delete sync info for all valid hashes
	for _, batch := range chunkInts(validHashIDs, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, id := range batch {
			args = append(args, id)
		}

		query := fmt.Sprintf(`DELETE FROM torrent_files_sync WHERE instance_id = ? AND torrent_hash_id IN (%s)`, buildPlaceholders(len(batch)))
		if _, err = tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("DeleteTorrentCache: failed to delete sync info: %w", err)
		}
	}

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("DeleteTorrentCache: commit failed: %w", commitErr)
	}

	return nil
}

// DeleteCacheForRemovedTorrents removes cache entries for torrents that no longer exist
func (r *Repository) DeleteCacheForRemovedTorrents(ctx context.Context, instanceID int, currentHashes []string) (int, error) {
	if len(currentHashes) == 0 {
		// If no current hashes provided, don't delete anything to be safe
		return 0, nil
	}

	// Deduplicate current hashes
	currentHashes = dedupeHashes(currentHashes)
	if len(currentHashes) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Create a temporary table for current hashes
	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE current_hashes (hash TEXT PRIMARY KEY)`)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp table: %w", err)
	}

	// Insert current hashes into temp table in batches
	queryTemplate := `INSERT INTO current_hashes (hash) VALUES %s ON CONFLICT(hash) DO NOTHING`
	const hashBatchSize = 900 // Keep under SQLite's 999 variable limit
	fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 1, hashBatchSize)

	args := make([]any, 0, hashBatchSize)

	for i := 0; i < len(currentHashes); i += hashBatchSize {
		end := i + hashBatchSize
		if end > len(currentHashes) {
			end = len(currentHashes)
		}
		batch := currentHashes[i:end]

		// Reset args for this batch
		args = args[:0]
		var query string
		if len(batch) == hashBatchSize {
			query = fullBatchQuery
		} else {
			// Build query for partial final batch
			query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 1, len(batch))
		}

		for _, hash := range batch {
			args = append(args, hash)
		}

		_, err = tx.ExecContext(ctx, query, args...)
		if err != nil {
			return 0, fmt.Errorf("failed to insert hashes batch into temp table: %w", err)
		}
	}

	// Delete files for torrents not in current hashes
	result, err := tx.ExecContext(ctx, `
		DELETE FROM torrent_files_cache
		WHERE instance_id = ? AND torrent_hash_id NOT IN (
			SELECT id FROM string_pool WHERE value IN (SELECT hash FROM current_hashes)
		)
	`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete files for removed torrents: %w", err)
	}

	// Delete sync info for torrents not in current hashes
	_, err = tx.ExecContext(ctx, `
		DELETE FROM torrent_files_sync
		WHERE instance_id = ? AND torrent_hash_id NOT IN (
			SELECT id FROM string_pool WHERE value IN (SELECT hash FROM current_hashes)
		)
	`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete sync info for removed torrents: %w", err)
	}

	// Drop the temporary table
	_, err = tx.ExecContext(ctx, `DROP TABLE current_hashes`)
	if err != nil {
		return 0, fmt.Errorf("failed to drop temp table: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	return int(rowsAffected), nil
}

// GetCacheStats returns statistics about the cache for an instance
func (r *Repository) GetCacheStats(ctx context.Context, instanceID int) (*CacheStats, error) {
	var stats CacheStats
	var (
		oldestSecs sql.NullFloat64
		newestSecs sql.NullFloat64
		avgSecs    sql.NullFloat64
		err        error
	)

	switch dbinterface.DialectOf(r.db) {
	case "postgres":
		now := time.Now().UTC()
		query := `
			SELECT
				COUNT(DISTINCT torrent_hash) as cached_torrents,
				COUNT(*) as total_files,
				MIN(EXTRACT(EPOCH FROM (? - last_synced_at))) as oldest_seconds,
				MAX(EXTRACT(EPOCH FROM (? - last_synced_at))) as newest_seconds,
				AVG(EXTRACT(EPOCH FROM (? - last_synced_at))) as avg_seconds
			FROM torrent_files_sync_view
			WHERE instance_id = ?
		`
		err = r.db.QueryRowContext(ctx, query, now, now, now, instanceID).Scan(
			&stats.CachedTorrents,
			&stats.TotalFiles,
			&oldestSecs,
			&newestSecs,
			&avgSecs,
		)
	default:
		nowJulian := float64(time.Now().UTC().UnixNano())/86400e9 + 2440587.5
		query := `
			SELECT
				COUNT(DISTINCT torrent_hash) as cached_torrents,
				COUNT(*) as total_files,
				MIN((? - julianday(last_synced_at)) * 86400) as oldest_seconds,
				MAX((? - julianday(last_synced_at)) * 86400) as newest_seconds,
				AVG((? - julianday(last_synced_at)) * 86400) as avg_seconds
			FROM torrent_files_sync_view
			WHERE instance_id = ?
		`
		err = r.db.QueryRowContext(ctx, query, nowJulian, nowJulian, nowJulian, instanceID).Scan(
			&stats.CachedTorrents,
			&stats.TotalFiles,
			&oldestSecs,
			&newestSecs,
			&avgSecs,
		)
	}
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

func dedupeHashes(hashes []string) []string {
	seen := make(map[string]struct{}, len(hashes))
	unique := make([]string, 0, len(hashes))
	for _, h := range hashes {
		h = strings.TrimSpace(h)
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		unique = append(unique, h)
	}
	return unique
}

func chunkHashes(hashes []string, size int) [][]string {
	if size <= 0 || len(hashes) == 0 {
		return nil
	}
	var chunks [][]string
	for start := 0; start < len(hashes); start += size {
		end := start + size
		if end > len(hashes) {
			end = len(hashes)
		}
		chunks = append(chunks, hashes[start:end])
	}
	return chunks
}

func chunkInts(ints []int64, size int) [][]int64 {
	if size <= 0 || len(ints) == 0 {
		return nil
	}
	var chunks [][]int64
	for start := 0; start < len(ints); start += size {
		end := start + size
		if end > len(ints) {
			end = len(ints)
		}
		chunks = append(chunks, ints[start:end])
	}
	return chunks
}

func buildPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	var sb strings.Builder
	for i := 0; i < count; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
	}
	return sb.String()
}
