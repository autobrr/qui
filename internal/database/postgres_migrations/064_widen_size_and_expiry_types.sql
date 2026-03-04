-- Align older baseline installations with 64-bit size/expiry columns.

-- Some of these columns are referenced by views created in 063_baseline.sql.
-- Postgres cannot ALTER COLUMN TYPE while dependent views exist, even if the
-- resulting type is unchanged. Drop/recreate the affected views around the
-- type changes.
DROP VIEW IF EXISTS instance_backup_items_view;
DROP VIEW IF EXISTS instance_backup_runs_view;
DROP VIEW IF EXISTS torrent_files_cache_view;

ALTER TABLE sessions
    ALTER COLUMN expiry TYPE DOUBLE PRECISION;

ALTER TABLE dir_scan_files
    ALTER COLUMN file_size TYPE BIGINT;

ALTER TABLE instance_backup_runs
    ALTER COLUMN total_bytes TYPE BIGINT;

ALTER TABLE instance_backup_items
    ALTER COLUMN size_bytes TYPE BIGINT;

ALTER TABLE orphan_scan_runs
    ALTER COLUMN bytes_reclaimed TYPE BIGINT;

ALTER TABLE orphan_scan_files
    ALTER COLUMN file_size TYPE BIGINT;

ALTER TABLE torrent_files_cache
    ALTER COLUMN size TYPE BIGINT;

ALTER TABLE torznab_torrent_cache
    ALTER COLUMN size_bytes TYPE BIGINT;

CREATE VIEW instance_backup_items_view AS
SELECT
    ibi.id,
    ibi.run_id,
    sp_hash.value as torrent_hash,
    sp_name.value as name,
    sp_cat.value as category,
    ibi.size_bytes,
    sp_archive.value as archive_rel_path,
    sp_infohash_v1.value as infohash_v1,
    sp_infohash_v2.value as infohash_v2,
    sp_tags.value as tags,
    sp_blob.value as torrent_blob_path,
    ibi.created_at
FROM instance_backup_items ibi
LEFT JOIN string_pool sp_hash ON ibi.torrent_hash_id = sp_hash.id
LEFT JOIN string_pool sp_name ON ibi.name_id = sp_name.id
LEFT JOIN string_pool sp_cat ON ibi.category_id = sp_cat.id
LEFT JOIN string_pool sp_archive ON ibi.archive_rel_path_id = sp_archive.id
LEFT JOIN string_pool sp_infohash_v1 ON ibi.infohash_v1_id = sp_infohash_v1.id
LEFT JOIN string_pool sp_infohash_v2 ON ibi.infohash_v2_id = sp_infohash_v2.id
LEFT JOIN string_pool sp_tags ON ibi.tags_id = sp_tags.id
LEFT JOIN string_pool sp_blob ON ibi.torrent_blob_path_id = sp_blob.id;

CREATE VIEW instance_backup_runs_view AS
SELECT
    ibr.id,
    ibr.instance_id,
    sp_kind.value AS kind,
    sp_status.value AS status,
    sp_requested_by.value AS requested_by,
    ibr.requested_at,
    ibr.started_at,
    ibr.completed_at,
    sp_archive.value AS archive_path,
    sp_manifest.value AS manifest_path,
    ibr.total_bytes,
    ibr.torrent_count,
    ibr.category_counts_json,
    ibr.categories_json,
    ibr.tags_json,
    sp_error.value AS error_message
FROM instance_backup_runs ibr
JOIN string_pool sp_kind ON ibr.kind_id = sp_kind.id
JOIN string_pool sp_status ON ibr.status_id = sp_status.id
JOIN string_pool sp_requested_by ON ibr.requested_by_id = sp_requested_by.id
LEFT JOIN string_pool sp_error ON ibr.error_message_id = sp_error.id
LEFT JOIN string_pool sp_archive ON ibr.archive_path_id = sp_archive.id
LEFT JOIN string_pool sp_manifest ON ibr.manifest_path_id = sp_manifest.id;

CREATE VIEW torrent_files_cache_view AS
SELECT
    tfc.id,
    tfc.instance_id,
    tfc.torrent_hash_id,
    sp_hash.value AS torrent_hash,
    tfc.file_index,
    tfc.name_id,
    sp_name.value AS name,
    tfc.size,
    tfc.progress,
    tfc.priority,
    tfc.is_seed,
    tfc.piece_range_start,
    tfc.piece_range_end,
    tfc.availability,
    tfc.cached_at
FROM torrent_files_cache tfc
LEFT JOIN string_pool sp_hash ON tfc.torrent_hash_id = sp_hash.id
LEFT JOIN string_pool sp_name ON tfc.name_id = sp_name.id;
