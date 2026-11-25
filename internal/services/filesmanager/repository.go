// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/dbinterface"
)

type Repository struct {
	db dbinterface.Querier
}

const maxBatchItems = 800

const fileBatchSize = 80

type fileRow struct {
	instanceID      int
	hashID          int64
	fileIndex       int
	nameID          int64
	size            int64
	progress        float64
	priority        int
	isSeed          any
	pieceRangeStart int64
	pieceRangeEnd   int64
	availability    float64
}

const filesBatchQuery = `
	SELECT tfc.id, tfc.instance_id, sp_hash.value, tfc.file_index, sp_name.value, tfc.size, tfc.progress,
	       tfc.priority, tfc.is_seed, tfc.piece_range_start, tfc.piece_range_end, tfc.availability, tfc.cached_at
	FROM string_pool sp_hash
	CROSS JOIN torrent_files_cache tfc
	LEFT JOIN string_pool sp_name ON tfc.name_id = sp_name.id
	WHERE tfc.instance_id = ? AND sp_hash.value IN (%s) AND tfc.torrent_hash_id = sp_hash.id
	ORDER BY sp_hash.value, tfc.file_index ASC
`

const syncInfoBatchQuery = `
	SELECT tfs.instance_id, sp_hash.value, tfs.last_synced_at, tfs.torrent_progress, tfs.file_count
	FROM string_pool sp_hash
	CROSS JOIN torrent_files_sync tfs
	WHERE tfs.instance_id = ? AND sp_hash.value IN (%s) AND tfs.torrent_hash_id = sp_hash.id
`

const entriesBatchQuery = `
	SELECT sp_hash.value, tfs.last_synced_at,
	       CASE WHEN tfs.print_synced_at = tfs.last_synced_at THEN tfs.maindata_print END,
	       tfc.file_index, sp_name.value, tfc.size, tfc.progress, tfc.priority, tfc.is_seed,
	       tfc.piece_range_start, tfc.piece_range_end, tfc.availability
	FROM string_pool sp_hash
	CROSS JOIN torrent_files_sync tfs
	JOIN torrent_files_cache tfc ON tfc.instance_id = tfs.instance_id AND tfc.torrent_hash_id = tfs.torrent_hash_id
	LEFT JOIN string_pool sp_name ON tfc.name_id = sp_name.id
	WHERE tfs.instance_id = ? AND sp_hash.value IN (%s) AND tfs.torrent_hash_id = sp_hash.id
	ORDER BY sp_hash.value, tfc.file_index ASC
`

type cacheEntryRows struct {
	files    []CachedFile
	print    *int64
	syncedAt time.Time
}

func NewRepository(db dbinterface.Querier) *Repository {
	return &Repository{db: db}
}

func (r *Repository) GetFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string][]CachedFile, error) {
	cleaned := dedupeHashes(hashes)
	if len(cleaned) == 0 {
		return map[string][]CachedFile{}, nil
	}

	results := make(map[string][]CachedFile, len(cleaned))
	for batch := range slices.Chunk(cleaned, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, h := range batch {
			args = append(args, h)
		}

		query := fmt.Sprintf(filesBatchQuery, buildPlaceholders(len(batch)))

		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var (
				f      CachedFile
				isSeed sql.NullInt64
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

			f.IsSeed = decodeNullableBoolFromInt(isSeed)

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

func (r *Repository) getEntriesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]*cacheEntryRows, error) {
	cleaned := dedupeHashes(hashes)
	results := make(map[string]*cacheEntryRows, len(cleaned))
	for batch := range slices.Chunk(cleaned, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, h := range batch {
			args = append(args, h)
		}

		rows, err := r.db.QueryContext(ctx, fmt.Sprintf(entriesBatchQuery, buildPlaceholders(len(batch))), args...)
		if err != nil {
			return nil, err
		}

		for rows.Next() {
			var (
				f        = CachedFile{InstanceID: instanceID}
				syncedAt time.Time
				stored   sql.NullInt64
				isSeed   sql.NullInt64
			)
			if err := rows.Scan(
				&f.TorrentHash,
				&syncedAt,
				&stored,
				&f.FileIndex,
				&f.Name,
				&f.Size,
				&f.Progress,
				&f.Priority,
				&isSeed,
				&f.PieceRangeStart,
				&f.PieceRangeEnd,
				&f.Availability,
			); err != nil {
				rows.Close()
				return nil, err
			}
			f.IsSeed = decodeNullableBoolFromInt(isSeed)

			entry := results[f.TorrentHash]
			if entry == nil {
				entry = &cacheEntryRows{syncedAt: syncedAt}
				if stored.Valid {
					entry.print = &stored.Int64
				}
				results[f.TorrentHash] = entry
			}
			entry.files = append(entry.files, f)
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

func (r *Repository) UpsertCache(ctx context.Context, files []CachedFile, infos []SyncInfo) error {
	if len(files) == 0 && len(infos) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := dbinterface.DeferForeignKeyChecks(ctx, tx); err != nil {
		return fmt.Errorf("failed to defer foreign keys: %w", err)
	}

	instanceIDs := make([]int, 0, len(infos)+len(files))
	allStrings := make([]string, 0, len(infos)+2*len(files))
	for _, info := range infos {
		instanceIDs = append(instanceIDs, info.InstanceID)
		allStrings = append(allStrings, info.TorrentHash)
	}
	for _, f := range files {
		instanceIDs = append(instanceIDs, f.InstanceID)
		allStrings = append(allStrings, f.TorrentHash, f.Name)
	}

	if err := r.lockInstances(ctx, tx, instanceIDs); err != nil {
		return err
	}

	allIDs, err := dbinterface.InternStrings(ctx, tx, allStrings...)
	if err != nil {
		return fmt.Errorf("failed to intern strings: %w", err)
	}
	valueToID := make(map[string]int64, len(allStrings))
	for i, value := range allStrings {
		valueToID[value] = allIDs[i]
	}

	if err := upsertSyncRows(ctx, tx, infos, valueToID); err != nil {
		return err
	}
	if err := upsertFiles(ctx, tx, files, valueToID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

func (r *Repository) lockInstances(ctx context.Context, tx dbinterface.TxQuerier, instanceIDs []int) error {
	if dbinterface.DialectOf(r.db) != "postgres" {
		return nil
	}
	ids := slices.Clone(instanceIDs)
	slices.Sort(ids)
	for _, id := range slices.Compact(ids) {
		if _, err := tx.ExecContext(ctx, `SELECT 1 FROM instances WHERE id = ? FOR KEY SHARE`, id); err != nil {
			return fmt.Errorf("failed to lock instance %d: %w", id, err)
		}
	}
	return nil
}

func upsertFiles(ctx context.Context, tx dbinterface.TxQuerier, files []CachedFile, valueToID map[string]int64) error {
	if len(files) == 0 {
		return nil
	}

	allRows := make([]fileRow, 0, len(files))
	for _, f := range files {
		allRows = append(allRows, fileRow{
			instanceID:      f.InstanceID,
			hashID:          valueToID[f.TorrentHash],
			fileIndex:       f.FileIndex,
			nameID:          valueToID[f.Name],
			size:            f.Size,
			progress:        f.Progress,
			priority:        f.Priority,
			isSeed:          encodeNullableBoolAsInt(f.IsSeed),
			pieceRangeStart: f.PieceRangeStart,
			pieceRangeEnd:   f.PieceRangeEnd,
			availability:    f.Availability,
		})
	}

	slices.SortFunc(allRows, func(a, b fileRow) int {
		return cmp.Or(cmp.Compare(a.instanceID, b.instanceID), cmp.Compare(a.hashID, b.hashID), cmp.Compare(a.fileIndex, b.fileIndex))
	})

	allRows, err := dropUnchangedRows(ctx, tx, allRows)
	if err != nil {
		return fmt.Errorf("failed to read cached files: %w", err)
	}

	return upsertFileRows(ctx, tx, allRows)
}

func upsertFileRows(ctx context.Context, tx dbinterface.TxQuerier, rows []fileRow) error {
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
			WHERE torrent_files_cache.name_id IS DISTINCT FROM excluded.name_id
				OR torrent_files_cache.size IS DISTINCT FROM excluded.size
				OR torrent_files_cache.progress IS DISTINCT FROM excluded.progress
				OR torrent_files_cache.priority IS DISTINCT FROM excluded.priority
				OR torrent_files_cache.is_seed IS DISTINCT FROM excluded.is_seed
				OR torrent_files_cache.piece_range_start IS DISTINCT FROM excluded.piece_range_start
				OR torrent_files_cache.piece_range_end IS DISTINCT FROM excluded.piece_range_end
				OR torrent_files_cache.availability IS DISTINCT FROM excluded.availability
		`
	fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 12, fileBatchSize)
	t := time.Now()

	args := make([]any, 0, fileBatchSize*12)

	for i := 0; i < len(rows); i += fileBatchSize {
		end := min(i+fileBatchSize, len(rows))
		batch := rows[i:end]

		args = args[:0]
		var query string
		if len(batch) == fileBatchSize {
			query = fullBatchQuery
		} else {
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

		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to batch insert files: %w", err)
		}
	}

	return nil
}

func dropUnchangedRows(ctx context.Context, tx dbinterface.TxQuerier, rows []fileRow) ([]fileRow, error) {
	type torrentKey struct {
		instanceID int
		hashID     int64
	}
	type fileKey struct {
		torrentKey
		fileIndex int
	}

	hashIDsByInstance := make(map[int][]int64)
	seen := make(map[torrentKey]struct{})
	for _, row := range rows {
		key := torrentKey{row.instanceID, row.hashID}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			hashIDsByInstance[row.instanceID] = append(hashIDsByInstance[row.instanceID], row.hashID)
		}
	}

	stored := make(map[fileKey]fileRow, len(rows))
	for instanceID, hashIDs := range hashIDsByInstance {
		for batch := range slices.Chunk(hashIDs, maxBatchItems) {
			args := make([]any, 0, len(batch)+1)
			args = append(args, instanceID)
			for _, id := range batch {
				args = append(args, id)
			}
			query := fmt.Sprintf(`
				SELECT torrent_hash_id, file_index, name_id, size, progress, priority,
				       is_seed, piece_range_start, piece_range_end, availability
				FROM torrent_files_cache
				WHERE instance_id = ? AND torrent_hash_id IN (%s)
			`, buildPlaceholders(len(batch)))

			dbRows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				return nil, err
			}
			for dbRows.Next() {
				row := fileRow{instanceID: instanceID}
				var isSeed sql.NullInt64
				if err := dbRows.Scan(&row.hashID, &row.fileIndex, &row.nameID, &row.size, &row.progress,
					&row.priority, &isSeed, &row.pieceRangeStart, &row.pieceRangeEnd, &row.availability); err != nil {
					dbRows.Close()
					return nil, err
				}
				row.isSeed = encodeNullableBoolAsInt(decodeNullableBoolFromInt(isSeed))
				stored[fileKey{torrentKey{instanceID, row.hashID}, row.fileIndex}] = row
			}
			if err := dbRows.Close(); err != nil {
				return nil, err
			}
			if err := dbRows.Err(); err != nil {
				return nil, err
			}
		}
	}

	changed := rows[:0]
	for _, row := range rows {
		if old, ok := stored[fileKey{torrentKey{row.instanceID, row.hashID}, row.fileIndex}]; !ok || old != row {
			changed = append(changed, row)
		}
	}
	return changed, nil
}

func (r *Repository) DeleteFiles(ctx context.Context, instanceID int, hash string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	hashIDs, err := dbinterface.GetStringID(ctx, tx, hash)
	if err != nil {
		return fmt.Errorf("failed to get torrent_hash ID: %w", err)
	}

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

	if rowsAffected, err := result.RowsAffected(); err == nil && rowsAffected > 0 {
		log.Debug().Int("instanceID", instanceID).Str("hash", hash).Int64("files", rowsAffected).
			Msg("Deleted cached files")
	}

	return nil
}

func encodeNullableBoolAsInt(value *bool) any {
	if value == nil {
		return nil
	}
	if *value {
		return 1
	}
	return 0
}

func decodeNullableBoolFromInt(value sql.NullInt64) *bool {
	if !value.Valid {
		return nil
	}

	result := value.Int64 != 0
	return &result
}

func (r *Repository) GetSyncInfoBatch(ctx context.Context, instanceID int, hashes []string) (map[string]*SyncInfo, error) {
	cleaned := dedupeHashes(hashes)
	if len(cleaned) == 0 {
		return map[string]*SyncInfo{}, nil
	}

	results := make(map[string]*SyncInfo, len(cleaned))
	for batch := range slices.Chunk(cleaned, maxBatchItems) {
		args := make([]any, 0, len(batch)+1)
		args = append(args, instanceID)
		for _, h := range batch {
			args = append(args, h)
		}

		query := fmt.Sprintf(syncInfoBatchQuery, buildPlaceholders(len(batch)))

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

func upsertSyncRows(ctx context.Context, tx dbinterface.TxQuerier, infos []SyncInfo, valueToID map[string]int64) error {
	if len(infos) == 0 {
		return nil
	}

	sorted := slices.Clone(infos)
	slices.SortFunc(sorted, func(a, b SyncInfo) int {
		return cmp.Or(cmp.Compare(a.InstanceID, b.InstanceID), cmp.Compare(valueToID[a.TorrentHash], valueToID[b.TorrentHash]))
	})

	const syncBatchSize = 128

	queryTemplate := `
		INSERT INTO torrent_files_sync
		(instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count, maindata_print, print_synced_at)
		VALUES %s
		ON CONFLICT(instance_id, torrent_hash_id) DO UPDATE SET
			last_synced_at = excluded.last_synced_at,
			torrent_progress = excluded.torrent_progress,
			file_count = excluded.file_count,
			maindata_print = excluded.maindata_print,
			print_synced_at = excluded.last_synced_at
	`
	fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 7, syncBatchSize)
	args := make([]any, 0, syncBatchSize*7)

	for batch := range slices.Chunk(sorted, syncBatchSize) {
		args = args[:0]
		query := fullBatchQuery
		if len(batch) < syncBatchSize {
			query = dbinterface.BuildQueryWithPlaceholders(queryTemplate, 7, len(batch))
		}

		for _, info := range batch {
			args = append(args,
				info.InstanceID,
				valueToID[info.TorrentHash],
				info.LastSyncedAt,
				info.TorrentProgress,
				info.FileCount,
				info.MaindataPrint,
				info.LastSyncedAt,
			)
		}

		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("failed to upsert sync info batch: %w", err)
		}
	}
	return nil
}

func (r *Repository) DeleteTorrentCache(ctx context.Context, instanceID int, hashes []string) error {
	if len(hashes) == 0 {
		return nil
	}

	hashes = dedupeHashes(hashes)
	if len(hashes) == 0 {
		return nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("DeleteTorrentCache: failed to begin transaction: %w", err)
	}
	defer func() {
		if rollErr := tx.Rollback(); rollErr != nil && !errors.Is(rollErr, sql.ErrTxDone) && err == nil {
			err = fmt.Errorf("DeleteTorrentCache: rollback failed: %w", rollErr)
		}
	}()

	if err = r.lockInstances(ctx, tx, []int{instanceID}); err != nil {
		return fmt.Errorf("DeleteTorrentCache: %w", err)
	}

	hashIDs, err := dbinterface.GetStringID(ctx, tx, hashes...)
	if err != nil {
		return fmt.Errorf("DeleteTorrentCache: failed to get torrent_hash IDs: %w", err)
	}

	validHashIDs := make([]int64, 0, len(hashIDs))
	for _, id := range hashIDs {
		if id.Valid {
			validHashIDs = append(validHashIDs, id.Int64)
		}
	}

	if len(validHashIDs) == 0 {
		if commitErr := tx.Commit(); commitErr != nil {
			return fmt.Errorf("DeleteTorrentCache: commit failed when no valid hashes: %w", commitErr)
		}
		return nil
	}

	for batch := range slices.Chunk(validHashIDs, maxBatchItems) {
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

	for batch := range slices.Chunk(validHashIDs, maxBatchItems) {
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

	if commitErr := tx.Commit(); commitErr != nil {
		return fmt.Errorf("DeleteTorrentCache: commit failed: %w", commitErr)
	}

	return nil
}

func (r *Repository) DeleteCacheForRemovedTorrents(ctx context.Context, instanceID int, currentHashes []string) (int, error) {
	if len(currentHashes) == 0 {
		return 0, nil
	}

	currentHashes = dedupeHashes(currentHashes)
	if len(currentHashes) == 0 {
		return 0, nil
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := r.lockInstances(ctx, tx, []int{instanceID}); err != nil {
		return 0, err
	}

	_, err = tx.ExecContext(ctx, `CREATE TEMP TABLE current_hashes (hash TEXT PRIMARY KEY)`)
	if err != nil {
		return 0, fmt.Errorf("failed to create temp table: %w", err)
	}

	queryTemplate := `INSERT INTO current_hashes (hash) VALUES %s ON CONFLICT(hash) DO NOTHING`
	const hashBatchSize = 900
	fullBatchQuery := dbinterface.BuildQueryWithPlaceholders(queryTemplate, 1, hashBatchSize)

	args := make([]any, 0, hashBatchSize)

	for i := 0; i < len(currentHashes); i += hashBatchSize {
		end := min(i+hashBatchSize, len(currentHashes))
		batch := currentHashes[i:end]

		args = args[:0]
		var query string
		if len(batch) == hashBatchSize {
			query = fullBatchQuery
		} else {
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

	if dbinterface.DialectOf(r.db) == "postgres" {
		_, err = tx.ExecContext(ctx, `
			SELECT torrent_hash_id FROM torrent_files_sync
			WHERE instance_id = ? AND torrent_hash_id NOT IN (
				SELECT id FROM string_pool WHERE value IN (SELECT hash FROM current_hashes)
			)
			ORDER BY torrent_hash_id
			FOR UPDATE
		`, instanceID)
		if err != nil {
			return 0, fmt.Errorf("failed to lock sync info for removed torrents: %w", err)
		}
	}

	_, err = tx.ExecContext(ctx, `
		DELETE FROM torrent_files_sync
		WHERE instance_id = ? AND torrent_hash_id NOT IN (
			SELECT id FROM string_pool WHERE value IN (SELECT hash FROM current_hashes)
		)
	`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete sync info for removed torrents: %w", err)
	}

	result, err := tx.ExecContext(ctx, `
		DELETE FROM torrent_files_cache
		WHERE instance_id = ? AND torrent_hash_id NOT IN (
			SELECT id FROM string_pool WHERE value IN (SELECT hash FROM current_hashes)
		)
	`, instanceID)
	if err != nil {
		return 0, fmt.Errorf("failed to delete files for removed torrents: %w", err)
	}

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
				MAX(EXTRACT(EPOCH FROM (? - last_synced_at))) as oldest_seconds,
				MIN(EXTRACT(EPOCH FROM (? - last_synced_at))) as newest_seconds,
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
				MAX((? - julianday(last_synced_at)) * 86400) as oldest_seconds,
				MIN((? - julianday(last_synced_at)) * 86400) as newest_seconds,
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
		h = strings.ToLower(strings.TrimSpace(h))
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

func buildPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	var sb strings.Builder
	for i := range count {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
	}
	return sb.String()
}
