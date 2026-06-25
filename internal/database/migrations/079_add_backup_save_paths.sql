-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Per-torrent save paths for backup/restore (issue #2061).
-- save_path is a plain TEXT column (not interned): the target case is
-- cross-seed hardlinks whose paths are unique per torrent and would only bloat
-- string_pool while adding daily-GC scan cost. Precedent: dir_scan_run_injections.save_path.
ALTER TABLE instance_backup_items ADD COLUMN save_path TEXT;

-- Optional capture toggle, default ON to match include_categories/include_tags.
ALTER TABLE instance_backup_settings ADD COLUMN include_save_paths BOOLEAN NOT NULL DEFAULT 1;

-- Recreate the items view to expose the new column (SQLite has no ALTER VIEW).
DROP VIEW IF EXISTS instance_backup_items_view;
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
    ibi.save_path,
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
