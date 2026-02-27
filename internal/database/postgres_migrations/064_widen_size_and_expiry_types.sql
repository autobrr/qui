-- Align older baseline installations with 64-bit size/expiry columns.

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
