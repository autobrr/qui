-- Migration 010: Add torrent files cache and comprehensive string interning
-- This migration combines:
-- - Torrent files caching to reduce API calls
-- - String interning system for deduplication
-- - Application of string interning to all repetitive text fields

-- ============================================================================
-- PART 1: Create string interning infrastructure
-- ============================================================================

-- String interning table to deduplicate repeated strings across the database
CREATE TABLE IF NOT EXISTS string_pool (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    value TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index for fast string lookups when inserting/checking for duplicates
CREATE UNIQUE INDEX IF NOT EXISTS idx_string_pool_value ON string_pool(value);

-- ============================================================================
-- PART 2: Create torrent files cache tables
-- ============================================================================

-- Cache torrent file information to reduce API calls to qBittorrent
-- Files for 100% complete torrents are assumed stable and don't need frequent refreshing
CREATE TABLE IF NOT EXISTS torrent_files_cache (
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

-- Index for fast lookups by instance and torrent hash
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_lookup ON torrent_files_cache(instance_id, torrent_hash_id);

-- Index for cache invalidation queries
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_cached_at ON torrent_files_cache(cached_at);

-- Store metadata about when each torrent's files were last synced
CREATE TABLE IF NOT EXISTS torrent_files_sync (
    instance_id INTEGER NOT NULL,
    torrent_hash_id INTEGER NOT NULL,
    last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    torrent_progress REAL NOT NULL DEFAULT 0,
    file_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance_id, torrent_hash_id),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (torrent_hash_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

-- Index for finding stale caches
CREATE INDEX IF NOT EXISTS idx_torrent_files_sync_last_synced ON torrent_files_sync(last_synced_at);

-- ============================================================================
-- PART 3: Migrate existing tables to use string interning
-- ============================================================================

-- Add temporary columns for string references to existing tables
ALTER TABLE instance_backup_items ADD COLUMN name_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN torrent_hash_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN category_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN tags_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN archive_rel_path_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN torrent_blob_path_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN infohash_v1_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_items ADD COLUMN infohash_v2_id INTEGER REFERENCES string_pool(id);

ALTER TABLE instance_errors ADD COLUMN error_type_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_errors ADD COLUMN error_message_id INTEGER REFERENCES string_pool(id);

ALTER TABLE instance_backup_runs ADD COLUMN kind_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN status_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN requested_by_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN error_message_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN archive_path_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN manifest_path_id INTEGER REFERENCES string_pool(id);

ALTER TABLE instances ADD COLUMN name_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instances ADD COLUMN host_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instances ADD COLUMN username_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instances ADD COLUMN basic_username_id INTEGER REFERENCES string_pool(id);

ALTER TABLE api_keys ADD COLUMN name_id INTEGER REFERENCES string_pool(id);

ALTER TABLE client_api_keys ADD COLUMN client_name_id INTEGER REFERENCES string_pool(id);

-- Populate string_pool with all unique string values from all tables
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT torrent_hash FROM instance_backup_items WHERE torrent_hash IS NOT NULL AND torrent_hash != ''
UNION
SELECT DISTINCT name FROM instance_backup_items WHERE name IS NOT NULL AND name != ''
UNION
SELECT DISTINCT category FROM instance_backup_items WHERE category IS NOT NULL AND category != ''
UNION
SELECT DISTINCT tags FROM instance_backup_items WHERE tags IS NOT NULL AND tags != ''
UNION
SELECT DISTINCT archive_rel_path FROM instance_backup_items WHERE archive_rel_path IS NOT NULL AND archive_rel_path != ''
UNION
SELECT DISTINCT torrent_blob_path FROM instance_backup_items WHERE torrent_blob_path IS NOT NULL AND torrent_blob_path != ''
UNION
SELECT DISTINCT infohash_v1 FROM instance_backup_items WHERE infohash_v1 IS NOT NULL AND infohash_v1 != ''
UNION
SELECT DISTINCT infohash_v2 FROM instance_backup_items WHERE infohash_v2 IS NOT NULL AND infohash_v2 != ''
UNION
SELECT DISTINCT error_type FROM instance_errors WHERE error_type IS NOT NULL AND error_type != ''
UNION
SELECT DISTINCT error_message FROM instance_errors WHERE error_message IS NOT NULL AND error_message != ''
UNION
SELECT DISTINCT kind FROM instance_backup_runs WHERE kind IS NOT NULL AND kind != ''
UNION
SELECT DISTINCT status FROM instance_backup_runs WHERE status IS NOT NULL AND status != ''
UNION
SELECT DISTINCT requested_by FROM instance_backup_runs WHERE requested_by IS NOT NULL AND requested_by != ''
UNION
SELECT DISTINCT error_message FROM instance_backup_runs WHERE error_message IS NOT NULL AND error_message != ''
UNION
SELECT DISTINCT archive_path FROM instance_backup_runs WHERE archive_path IS NOT NULL AND archive_path != ''
UNION
SELECT DISTINCT manifest_path FROM instance_backup_runs WHERE manifest_path IS NOT NULL AND manifest_path != ''
UNION
SELECT DISTINCT name FROM instances WHERE name IS NOT NULL AND name != ''
UNION
SELECT DISTINCT host FROM instances WHERE host IS NOT NULL AND host != ''
UNION
SELECT DISTINCT username FROM instances WHERE username IS NOT NULL AND username != ''
UNION
SELECT DISTINCT basic_username FROM instances WHERE basic_username IS NOT NULL AND basic_username != ''
UNION
SELECT DISTINCT name FROM api_keys WHERE name IS NOT NULL AND name != ''
UNION
SELECT DISTINCT client_name FROM client_api_keys WHERE client_name IS NOT NULL AND client_name != '';

-- Insert placeholder values for empty/NULL strings
INSERT OR IGNORE INTO string_pool (value) VALUES ('(unknown)');
INSERT OR IGNORE INTO string_pool (value) VALUES ('(unnamed)');

-- Update all foreign key references
UPDATE instance_backup_items
SET name_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_backup_items.name),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_backup_items
SET torrent_hash_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_backup_items.torrent_hash),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_backup_items
SET category_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.category)
WHERE category IS NOT NULL AND category != '';

UPDATE instance_backup_items
SET tags_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.tags)
WHERE tags IS NOT NULL AND tags != '';

UPDATE instance_backup_items
SET archive_rel_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.archive_rel_path)
WHERE archive_rel_path IS NOT NULL AND archive_rel_path != '';

UPDATE instance_backup_items
SET torrent_blob_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.torrent_blob_path)
WHERE torrent_blob_path IS NOT NULL AND torrent_blob_path != '';

UPDATE instance_backup_items
SET infohash_v1_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.infohash_v1)
WHERE infohash_v1 IS NOT NULL AND infohash_v1 != '';

UPDATE instance_backup_items
SET infohash_v2_id = (SELECT id FROM string_pool WHERE value = instance_backup_items.infohash_v2)
WHERE infohash_v2 IS NOT NULL AND infohash_v2 != '';

UPDATE instance_errors
SET error_type_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_errors.error_type),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_errors
SET error_message_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_errors.error_message),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_backup_runs
SET kind_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_backup_runs.kind),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_backup_runs
SET status_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_backup_runs.status),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_backup_runs
SET requested_by_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instance_backup_runs.requested_by),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instance_backup_runs
SET error_message_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.error_message)
WHERE error_message IS NOT NULL AND error_message != '';

UPDATE instance_backup_runs
SET archive_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.archive_path)
WHERE archive_path IS NOT NULL AND archive_path != '';

UPDATE instance_backup_runs
SET manifest_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.manifest_path)
WHERE manifest_path IS NOT NULL AND manifest_path != '';

UPDATE instances
SET name_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instances.name),
    (SELECT id FROM string_pool WHERE value = '(unnamed)')
);

UPDATE instances
SET host_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instances.host),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instances
SET username_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = instances.username),
    (SELECT id FROM string_pool WHERE value = '(unknown)')
);

UPDATE instances
SET basic_username_id = (SELECT id FROM string_pool WHERE value = instances.basic_username)
WHERE basic_username IS NOT NULL AND basic_username != '';

UPDATE api_keys
SET name_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = api_keys.name),
    (SELECT id FROM string_pool WHERE value = '(unnamed)')
);

UPDATE client_api_keys
SET client_name_id = COALESCE(
    (SELECT id FROM string_pool WHERE value = client_api_keys.client_name),
    (SELECT id FROM string_pool WHERE value = '(unnamed)')
);

-- ============================================================================
-- PART 4: Recreate tables with string interning (drop old TEXT columns)
-- ============================================================================

-- Recreate instance_backup_items table
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

INSERT INTO instance_backup_items_new (id, run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1_id, infohash_v2_id, tags_id, torrent_blob_path_id, created_at)
SELECT id, run_id, torrent_hash_id, name_id, category_id, size_bytes, archive_rel_path_id, infohash_v1_id, infohash_v2_id, tags_id, torrent_blob_path_id, created_at
FROM instance_backup_items;

DROP TABLE instance_backup_items;
ALTER TABLE instance_backup_items_new RENAME TO instance_backup_items;

CREATE INDEX IF NOT EXISTS idx_backup_items_run ON instance_backup_items(run_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_hash ON instance_backup_items(torrent_hash_id);

-- Recreate instance_errors table
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

INSERT INTO instance_errors_new (id, instance_id, error_type_id, error_message_id, occurred_at)
SELECT id, instance_id, error_type_id, error_message_id, occurred_at
FROM instance_errors;

DROP TABLE instance_errors;
ALTER TABLE instance_errors_new RENAME TO instance_errors;

CREATE INDEX IF NOT EXISTS idx_instance_errors_lookup ON instance_errors(instance_id, occurred_at DESC);

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

-- Recreate instance_backup_runs table
CREATE TABLE IF NOT EXISTS instance_backup_runs_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    kind_id INTEGER NOT NULL,
    status_id INTEGER NOT NULL,
    requested_by_id INTEGER NOT NULL,
    requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    archive_path_id INTEGER,
    manifest_path_id INTEGER,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    torrent_count INTEGER NOT NULL DEFAULT 0,
    category_counts_json TEXT,
    categories_json TEXT,
    tags_json TEXT,
    error_message_id INTEGER,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (kind_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (status_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (requested_by_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (error_message_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (archive_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (manifest_path_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

INSERT INTO instance_backup_runs_new (id, instance_id, kind_id, status_id, requested_by_id, requested_at, started_at, completed_at, archive_path_id, manifest_path_id, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message_id)
SELECT id, instance_id, kind_id, status_id, requested_by_id, requested_at, started_at, completed_at, archive_path_id, manifest_path_id, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message_id
FROM instance_backup_runs;

DROP TABLE instance_backup_runs;
ALTER TABLE instance_backup_runs_new RENAME TO instance_backup_runs;

CREATE INDEX IF NOT EXISTS idx_instance_backup_runs_instance ON instance_backup_runs(instance_id, requested_at DESC);

-- Recreate instances table
CREATE TABLE IF NOT EXISTS instances_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name_id INTEGER NOT NULL,
    host_id INTEGER NOT NULL,
    username_id INTEGER NOT NULL,
    password_encrypted TEXT NOT NULL,
    basic_username_id INTEGER,
    basic_password_encrypted TEXT,
    tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (host_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (username_id) REFERENCES string_pool(id) ON DELETE RESTRICT,
    FOREIGN KEY (basic_username_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

INSERT INTO instances_new (id, name_id, host_id, username_id, password_encrypted, basic_username_id, basic_password_encrypted, tls_skip_verify)
SELECT id, name_id, host_id, username_id, password_encrypted, basic_username_id, basic_password_encrypted, tls_skip_verify
FROM instances;

DROP TABLE instances;
ALTER TABLE instances_new RENAME TO instances;

-- Recreate api_keys table
CREATE TABLE IF NOT EXISTS api_keys_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash TEXT UNIQUE NOT NULL,
    name_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

INSERT INTO api_keys_new (id, key_hash, name_id, created_at, last_used_at)
SELECT id, key_hash, name_id, created_at, last_used_at
FROM api_keys;

DROP TABLE api_keys;
ALTER TABLE api_keys_new RENAME TO api_keys;

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);

-- Recreate client_api_keys table
CREATE TABLE IF NOT EXISTS client_api_keys_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key_hash TEXT NOT NULL UNIQUE,
    client_name_id INTEGER NOT NULL,
    instance_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (client_name_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

INSERT INTO client_api_keys_new (id, key_hash, client_name_id, instance_id, created_at, last_used_at)
SELECT id, key_hash, client_name_id, instance_id, created_at, last_used_at
FROM client_api_keys;

DROP TABLE client_api_keys;
ALTER TABLE client_api_keys_new RENAME TO client_api_keys;

CREATE UNIQUE INDEX IF NOT EXISTS idx_client_api_keys_key_hash ON client_api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_client_api_keys_instance_id ON client_api_keys(instance_id);

-- ============================================================================
-- PART 5: Create views for easier querying with automatic string resolution
-- ============================================================================

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

-- View for instances that automatically joins with string_pool
CREATE VIEW IF NOT EXISTS instances_view AS
SELECT 
    i.id,
    sp_name.value AS name,
    sp_host.value AS host,
    sp_username.value AS username,
    i.password_encrypted,
    sp_basic_username.value AS basic_username,
    i.basic_password_encrypted,
    i.tls_skip_verify
FROM instances i
INNER JOIN string_pool sp_name ON i.name_id = sp_name.id
INNER JOIN string_pool sp_host ON i.host_id = sp_host.id
INNER JOIN string_pool sp_username ON i.username_id = sp_username.id
LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;

-- View for api_keys that automatically joins with string_pool
CREATE VIEW IF NOT EXISTS api_keys_view AS
SELECT 
    ak.id,
    ak.key_hash,
    sp.value AS name,
    ak.created_at,
    ak.last_used_at
FROM api_keys ak
INNER JOIN string_pool sp ON ak.name_id = sp.id;

-- View for client_api_keys that automatically joins with string_pool
CREATE VIEW IF NOT EXISTS client_api_keys_view AS
SELECT 
    cak.id,
    cak.key_hash,
    sp.value AS client_name,
    cak.instance_id,
    cak.created_at,
    cak.last_used_at
FROM client_api_keys cak
INNER JOIN string_pool sp ON cak.client_name_id = sp.id;

-- View for instance_backup_runs that automatically joins with string_pool
CREATE VIEW IF NOT EXISTS instance_backup_runs_view AS
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
