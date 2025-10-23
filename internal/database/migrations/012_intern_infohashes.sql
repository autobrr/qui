-- Migration 012: Intern infohash_v1 and infohash_v2 columns
-- These are repetitive hash strings that should be deduplicated via string_pool

-- Add temporary columns for infohash string references
ALTER TABLE instance_backup_items ADD COLUMN infohash_v1_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN infohash_v2_id INTEGER REFERENCES string_pool(id);

-- Populate string_pool with unique infohashes
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT infohash_v1 FROM instance_backup_items WHERE infohash_v1 IS NOT NULL AND infohash_v1 != ''
UNION
SELECT DISTINCT infohash_v2 FROM instance_backup_items WHERE infohash_v2 IS NOT NULL AND infohash_v2 != '';

-- Update infohash_v1_id references
UPDATE instance_backup_items
SET infohash_v1_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.infohash_v1)
WHERE infohash_v1 IS NOT NULL AND infohash_v1 != '';

-- Update infohash_v2_id references
UPDATE instance_backup_items
SET infohash_v2_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.infohash_v2)
WHERE infohash_v2 IS NOT NULL AND infohash_v2 != '';

-- Drop the view before recreating the table
DROP VIEW IF EXISTS instance_backup_items_view;

-- Recreate table with infohash_v1_id and infohash_v2_id instead of TEXT columns
CREATE TABLE IF NOT EXISTS instance_backup_items_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL,
    torrent_hash_id INTEGER NOT NULL,
    name_id INTEGER NOT NULL,
    category_id INTEGER,
    size_bytes INTEGER NOT NULL,
    archive_rel_path_id INTEGER,
    infohash_v1_id INTEGER,
    infohash_v2_id INTEGER,
    tags_id INTEGER,
    torrent_blob_path_id INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES instance_backup_runs(id) ON DELETE CASCADE,
    FOREIGN KEY (torrent_hash_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (category_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (tags_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (archive_rel_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (infohash_v1_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (infohash_v2_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (torrent_blob_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

-- Copy data from old table to new table
INSERT INTO instance_backup_items_new (id, run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1_id, infohash_v2_id, tags_id, torrent_blob_path_id, created_at)
SELECT id, run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1_id, infohash_v2_id, tags_id, torrent_blob_path_id, created_at
FROM instance_backup_items;

-- Drop old table and rename new table
DROP TABLE instance_backup_items;
ALTER TABLE instance_backup_items_new RENAME TO instance_backup_items;

-- Recreate indexes
CREATE INDEX IF NOT EXISTS idx_backup_items_run ON instance_backup_items(run_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_hash ON instance_backup_items(torrent_hash_id);

-- Drop and recreate the view to use infohash_v1_id and infohash_v2_id
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
