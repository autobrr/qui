-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Store each torrent's backup state once, valid over a range of runs, instead
-- of one row per torrent per run. Every run used to rewrite and later delete
-- one row per torrent even when nothing changed.
--
-- items_seq orders a run's snapshot within its instance and is assigned when
-- the run's items commit. An item row belongs to every run of its instance with
-- from_seq <= items_seq < to_seq; to_seq is NULL while the state is current.
ALTER TABLE instance_backup_runs ADD COLUMN items_seq BIGINT;

UPDATE instance_backup_runs
SET items_seq = numbered.seq
FROM (
    SELECT r.id, ROW_NUMBER() OVER (PARTITION BY r.instance_id ORDER BY r.id) AS seq
    FROM instance_backup_runs r
    WHERE EXISTS (SELECT 1 FROM instance_backup_items i WHERE i.run_id = r.id)
) AS numbered
WHERE instance_backup_runs.id = numbered.id;

CREATE UNIQUE INDEX idx_instance_backup_runs_items_seq ON instance_backup_runs(instance_id, items_seq);

DROP VIEW IF EXISTS instance_backup_items_view;

ALTER TABLE instance_backup_items
    ADD COLUMN instance_id INTEGER,
    ADD COLUMN from_seq BIGINT,
    ADD COLUMN to_seq BIGINT;

-- Existing rows keep their one-run range. The newest snapshot per instance
-- stays open so the next run only writes what changed; older copies expire
-- with retention.
UPDATE instance_backup_items i
SET instance_id = r.instance_id,
    from_seq = r.items_seq,
    to_seq = CASE WHEN r.items_seq = latest.max_seq THEN NULL ELSE r.items_seq + 1 END
FROM instance_backup_runs r
JOIN (
    SELECT instance_id, MAX(items_seq) AS max_seq
    FROM instance_backup_runs
    GROUP BY instance_id
) AS latest ON latest.instance_id = r.instance_id
WHERE r.id = i.run_id;

ALTER TABLE instance_backup_items
    ALTER COLUMN instance_id SET NOT NULL,
    ALTER COLUMN from_seq SET NOT NULL,
    DROP COLUMN run_id,
    ADD FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_backup_items_hash;
CREATE INDEX idx_backup_items_hash ON instance_backup_items(torrent_hash_id, instance_id, from_seq);
CREATE INDEX idx_backup_items_instance_seq ON instance_backup_items(instance_id, from_seq);

CREATE VIEW instance_backup_items_view AS
SELECT
    ibi.id,
    ibi.instance_id,
    ibi.from_seq,
    ibi.to_seq,
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
