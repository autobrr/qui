-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Store each torrent's backup state once, valid over a range of runs, instead
-- of one row per torrent per run. Every run used to rewrite and later delete
-- one row per torrent even when nothing changed.
--
-- items_seq orders a run's snapshot within its instance and is assigned when
-- the run's items commit. An item row belongs to every run of its instance with
-- from_seq <= items_seq < to_seq; to_seq is NULL while the state is current.
ALTER TABLE instance_backup_runs ADD COLUMN items_seq INTEGER;

UPDATE instance_backup_runs
SET items_seq = numbered.seq
FROM (
    SELECT r.id, ROW_NUMBER() OVER (PARTITION BY r.instance_id ORDER BY r.id) AS seq
    FROM instance_backup_runs r
    WHERE EXISTS (SELECT 1 FROM instance_backup_items i WHERE i.run_id = r.id)
) AS numbered
WHERE instance_backup_runs.id = numbered.id;

CREATE UNIQUE INDEX idx_instance_backup_runs_items_seq ON instance_backup_runs(instance_id, items_seq);

CREATE TABLE instance_backup_items_ranged (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    from_seq INTEGER NOT NULL,
    to_seq INTEGER,
    torrent_hash_id INTEGER NOT NULL,
    name_id INTEGER NOT NULL,
    category_id INTEGER,
    size_bytes INTEGER NOT NULL,
    archive_rel_path_id INTEGER,
    infohash_v1_id INTEGER,
    infohash_v2_id INTEGER,
    tags_id INTEGER,
    torrent_blob_path_id INTEGER,
    save_path TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (torrent_hash_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (category_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (tags_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (archive_rel_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (infohash_v1_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (infohash_v2_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (torrent_blob_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

-- Existing rows keep their one-run range. The newest snapshot per instance
-- stays open so the next run only writes what changed; older copies expire
-- with retention.
INSERT INTO instance_backup_items_ranged (
    id, instance_id, from_seq, to_seq, torrent_hash_id, name_id, category_id, size_bytes,
    archive_rel_path_id, infohash_v1_id, infohash_v2_id, tags_id, torrent_blob_path_id, save_path, created_at
)
SELECT
    i.id, r.instance_id, r.items_seq,
    CASE WHEN r.items_seq = latest.max_seq THEN NULL ELSE r.items_seq + 1 END,
    i.torrent_hash_id, i.name_id, i.category_id, i.size_bytes,
    i.archive_rel_path_id, i.infohash_v1_id, i.infohash_v2_id, i.tags_id, i.torrent_blob_path_id, i.save_path, i.created_at
FROM instance_backup_items i
JOIN instance_backup_runs r ON r.id = i.run_id
JOIN (
    SELECT instance_id, MAX(items_seq) AS max_seq
    FROM instance_backup_runs
    GROUP BY instance_id
) AS latest ON latest.instance_id = r.instance_id;

DROP VIEW IF EXISTS instance_backup_items_view;
DROP TABLE instance_backup_items;
ALTER TABLE instance_backup_items_ranged RENAME TO instance_backup_items;

CREATE INDEX idx_backup_items_instance_seq ON instance_backup_items(instance_id, from_seq);
CREATE INDEX idx_backup_items_hash ON instance_backup_items(torrent_hash_id, instance_id, from_seq);
CREATE INDEX idx_backup_items_name_id ON instance_backup_items(name_id);
CREATE INDEX idx_backup_items_category_id ON instance_backup_items(category_id);
CREATE INDEX idx_backup_items_tags_id ON instance_backup_items(tags_id);
CREATE INDEX idx_backup_items_archive_rel_path_id ON instance_backup_items(archive_rel_path_id);
CREATE INDEX idx_backup_items_infohash_v1_id ON instance_backup_items(infohash_v1_id);
CREATE INDEX idx_backup_items_infohash_v2_id ON instance_backup_items(infohash_v2_id);
CREATE INDEX idx_backup_items_torrent_blob_path_id ON instance_backup_items(torrent_blob_path_id);

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
