-- String interning table to deduplicate repeated strings across the database
-- This significantly reduces storage for millions of torrent names, categories, tags, etc.
CREATE TABLE IF NOT EXISTS string_pool (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    value TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index for fast string lookups when inserting/checking for duplicates
CREATE UNIQUE INDEX IF NOT EXISTS idx_string_pool_value ON string_pool(value);

-- Migration: Create temporary columns for string references
ALTER TABLE torrent_files_cache ADD COLUMN name_id INTEGER REFERENCES string_pool(id);
ALTER TABLE torrent_files_cache ADD COLUMN torrent_hash_id INTEGER REFERENCES string_pool(id);
ALTER TABLE torrent_files_sync ADD COLUMN torrent_hash_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN name_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN torrent_hash_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN category_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN tags_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN archive_rel_path_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN torrent_blob_path_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_errors ADD COLUMN error_type_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_errors ADD COLUMN error_message_id INTEGER REFERENCES string_pool(id);

-- Populate string_pool with unique torrent hashes
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT torrent_hash FROM torrent_files_cache WHERE torrent_hash IS NOT NULL
UNION
SELECT DISTINCT torrent_hash FROM torrent_files_sync WHERE torrent_hash IS NOT NULL
UNION
SELECT DISTINCT torrent_hash FROM instance_backup_items WHERE torrent_hash IS NOT NULL;

-- Update torrent_hash_id references
UPDATE torrent_files_cache
SET torrent_hash_id = (SELECT id FROM string_pool WHERE value = torrent_files_cache.torrent_hash)
WHERE torrent_hash IS NOT NULL;

UPDATE torrent_files_sync
SET torrent_hash_id = (SELECT id FROM string_pool WHERE value = torrent_files_sync.torrent_hash)
WHERE torrent_hash IS NOT NULL;

UPDATE instance_backup_items
SET torrent_hash_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.torrent_hash)
WHERE torrent_hash IS NOT NULL;

-- Populate string_pool with unique values from torrent_files_cache.name
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT name FROM torrent_files_cache WHERE name IS NOT NULL;

-- Update torrent_files_cache.name_id to reference string_pool
UPDATE torrent_files_cache
SET name_id = (SELECT id FROM string_pool WHERE value = torrent_files_cache.name)
WHERE name IS NOT NULL;

-- Populate string_pool with unique values from instance_backup_items
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT name FROM instance_backup_items WHERE name IS NOT NULL;

INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT category FROM instance_backup_items WHERE category IS NOT NULL;

INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT tags FROM instance_backup_items WHERE tags IS NOT NULL;

INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT archive_rel_path FROM instance_backup_items WHERE archive_rel_path IS NOT NULL;

INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT torrent_blob_path FROM instance_backup_items WHERE torrent_blob_path IS NOT NULL;

-- Update instance_backup_items foreign key columns
UPDATE instance_backup_items
SET name_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.name)
WHERE name IS NOT NULL;

UPDATE instance_backup_items
SET category_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.category)
WHERE category IS NOT NULL;

UPDATE instance_backup_items
SET tags_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.tags)
WHERE tags IS NOT NULL;

UPDATE instance_backup_items
SET archive_rel_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.archive_rel_path)
WHERE archive_rel_path IS NOT NULL;

UPDATE instance_backup_items
SET torrent_blob_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.torrent_blob_path)
WHERE torrent_blob_path IS NOT NULL;

-- Populate string_pool with unique values from instance_errors
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT error_type FROM instance_errors WHERE error_type IS NOT NULL;

INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT error_message FROM instance_errors WHERE error_message IS NOT NULL;

-- Update instance_errors foreign key columns
UPDATE instance_errors
SET error_type_id = (SELECT id FROM string_pool WHERE value = instance_errors.error_type)
WHERE error_type IS NOT NULL;

UPDATE instance_errors
SET error_message_id = (SELECT id FROM string_pool WHERE value = instance_errors.error_message)
WHERE error_message IS NOT NULL;

-- Create new tables with string interning
CREATE TABLE IF NOT EXISTS torrent_files_cache_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    torrent_hash_id INTEGER NOT NULL,
    file_index INTEGER NOT NULL,
    name_id INTEGER NOT NULL,
    size INTEGER NOT NULL,
    progress REAL NOT NULL,
    priority INTEGER NOT NULL,
    is_seed INTEGER,
    piece_range_start INTEGER,
    piece_range_end INTEGER,
    availability REAL NOT NULL,
    cached_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (torrent_hash_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    UNIQUE(instance_id, torrent_hash_id, file_index)
);

CREATE TABLE IF NOT EXISTS torrent_files_sync_new (
    instance_id INTEGER NOT NULL,
    torrent_hash_id INTEGER NOT NULL,
    last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    torrent_progress REAL NOT NULL DEFAULT 0,
    file_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance_id, torrent_hash_id),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (torrent_hash_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS instance_backup_items_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL,
    torrent_hash_id INTEGER NOT NULL,
    name_id INTEGER NOT NULL,
    category_id INTEGER,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    archive_rel_path_id INTEGER,
    infohash_v1 TEXT,
    infohash_v2 TEXT,
    tags_id INTEGER,
    torrent_blob_path_id INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES instance_backup_runs(id) ON DELETE CASCADE,
    FOREIGN KEY (torrent_hash_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (category_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (tags_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (archive_rel_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (torrent_blob_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

CREATE TABLE IF NOT EXISTS instance_errors_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    error_type_id INTEGER NOT NULL,
    error_message_id INTEGER NOT NULL,
    occurred_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (error_type_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (error_message_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

-- Copy data from old tables to new tables
INSERT INTO torrent_files_cache_new (id, instance_id, torrent_hash_id, file_index, name_id, size, progress, priority, is_seed, piece_range_start, piece_range_end, availability, cached_at)
SELECT id, instance_id, torrent_hash_id, file_index, name_id, size, progress, priority, is_seed, piece_range_start, piece_range_end, availability, cached_at
FROM torrent_files_cache;

INSERT INTO torrent_files_sync_new (instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count)
SELECT instance_id, torrent_hash_id, last_synced_at, torrent_progress, file_count
FROM torrent_files_sync;

INSERT INTO instance_backup_items_new (id, run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1, infohash_v2, tags_id, torrent_blob_path_id, created_at)
SELECT id, run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1, infohash_v2, tags_id, torrent_blob_path_id, created_at
FROM instance_backup_items;

INSERT INTO instance_errors_new (id, instance_id, error_type_id, error_message_id, occurred_at)
SELECT id, instance_id, error_type_id, error_message_id, occurred_at
FROM instance_errors;

-- Drop old tables
DROP TABLE torrent_files_cache;
DROP TABLE torrent_files_sync;
DROP TABLE instance_backup_items;
DROP TABLE instance_errors;

-- Rename new tables to original names
ALTER TABLE torrent_files_cache_new RENAME TO torrent_files_cache;
ALTER TABLE torrent_files_sync_new RENAME TO torrent_files_sync;
ALTER TABLE instance_backup_items_new RENAME TO instance_backup_items;
ALTER TABLE instance_errors_new RENAME TO instance_errors;

-- Recreate indexes for torrent_files_cache
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_lookup ON torrent_files_cache(instance_id, torrent_hash_id);
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_cached_at ON torrent_files_cache(cached_at);

-- Recreate indexes for torrent_files_sync
CREATE INDEX IF NOT EXISTS idx_torrent_files_sync_last_synced ON torrent_files_sync(last_synced_at);

-- Recreate indexes for instance_backup_items
CREATE INDEX IF NOT EXISTS idx_instance_backup_items_run ON instance_backup_items(run_id);
CREATE INDEX IF NOT EXISTS idx_instance_backup_items_hash ON instance_backup_items(run_id, torrent_hash_id);

-- Recreate indexes for instance_errors
CREATE INDEX IF NOT EXISTS idx_instance_errors_lookup ON instance_errors(instance_id, occurred_at DESC);

-- Recreate the cleanup trigger for instance_errors
CREATE TRIGGER IF NOT EXISTS cleanup_old_instance_errors
AFTER INSERT ON instance_errors
BEGIN
    DELETE FROM instance_errors
    WHERE instance_id = NEW.instance_id
    AND id NOT IN (
        SELECT id FROM instance_errors
        WHERE instance_id = NEW.instance_id
        ORDER BY occurred_at DESC
        LIMIT 5
    );
END;

-- Create views to simplify querying with automatic string resolution
-- These views eliminate the need for manual JOINs with string_pool

-- View for torrent_files_cache with resolved string values
CREATE VIEW IF NOT EXISTS torrent_files_cache_view AS
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

-- View for torrent_files_sync with resolved string values
CREATE VIEW IF NOT EXISTS torrent_files_sync_view AS
SELECT
    tfs.instance_id,
    tfs.torrent_hash_id,
    sp_hash.value AS torrent_hash,
    tfs.last_synced_at,
    tfs.torrent_progress,
    tfs.file_count
FROM torrent_files_sync tfs
LEFT JOIN string_pool sp_hash ON tfs.torrent_hash_id = sp_hash.id;

-- View for instance_backup_items with resolved string values
CREATE VIEW IF NOT EXISTS instance_backup_items_view AS
SELECT
    ibi.id,
    ibi.run_id,
    ibi.torrent_hash_id,
    sp_hash.value AS torrent_hash,
    ibi.name_id,
    sp_name.value AS name,
    ibi.category_id,
    sp_category.value AS category,
    ibi.size_bytes,
    ibi.archive_rel_path_id,
    sp_archive.value AS archive_rel_path,
    ibi.infohash_v1,
    ibi.infohash_v2,
    ibi.tags_id,
    sp_tags.value AS tags,
    ibi.torrent_blob_path_id,
    sp_blob.value AS torrent_blob_path,
    ibi.created_at
FROM instance_backup_items ibi
LEFT JOIN string_pool sp_hash ON ibi.torrent_hash_id = sp_hash.id
LEFT JOIN string_pool sp_name ON ibi.name_id = sp_name.id
LEFT JOIN string_pool sp_category ON ibi.category_id = sp_category.id
LEFT JOIN string_pool sp_archive ON ibi.archive_rel_path_id = sp_archive.id
LEFT JOIN string_pool sp_tags ON ibi.tags_id = sp_tags.id
LEFT JOIN string_pool sp_blob ON ibi.torrent_blob_path_id = sp_blob.id;

-- View for instance_errors with resolved string values
CREATE VIEW IF NOT EXISTS instance_errors_view AS
SELECT
    ie.id,
    ie.instance_id,
    ie.error_type_id,
    sp_type.value AS error_type,
    ie.error_message_id,
    sp_message.value AS error_message,
    ie.occurred_at
FROM instance_errors ie
LEFT JOIN string_pool sp_type ON ie.error_type_id = sp_type.id
LEFT JOIN string_pool sp_message ON ie.error_message_id = sp_message.id;
