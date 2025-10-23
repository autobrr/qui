-- Migration 013: Intern repetitive strings in instance_backup_runs and other tables
-- Intern: kind, status, requested_by, error_message, instance names, api key names, client names

-- Add temporary columns for string references
ALTER TABLE instance_backup_runs ADD COLUMN kind_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN status_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN requested_by_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN error_message_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instances ADD COLUMN name_id INTEGER REFERENCES string_pool(id);
ALTER TABLE api_keys ADD COLUMN name_id INTEGER REFERENCES string_pool(id);
ALTER TABLE client_api_keys ADD COLUMN client_name_id INTEGER REFERENCES string_pool(id);

-- Populate string_pool with unique values
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT kind FROM instance_backup_runs WHERE kind IS NOT NULL
UNION
SELECT DISTINCT status FROM instance_backup_runs WHERE status IS NOT NULL
UNION
SELECT DISTINCT requested_by FROM instance_backup_runs WHERE requested_by IS NOT NULL
UNION
SELECT DISTINCT error_message FROM instance_backup_runs WHERE error_message IS NOT NULL AND error_message != ''
UNION
SELECT DISTINCT name FROM instances WHERE name IS NOT NULL
UNION
SELECT DISTINCT name FROM api_keys WHERE name IS NOT NULL
UNION
SELECT DISTINCT client_name FROM client_api_keys WHERE client_name IS NOT NULL;

-- Update references for instance_backup_runs
UPDATE instance_backup_runs
SET kind_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.kind)
WHERE kind IS NOT NULL;

UPDATE instance_backup_runs
SET status_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.status)
WHERE status IS NOT NULL;

UPDATE instance_backup_runs
SET requested_by_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.requested_by)
WHERE requested_by IS NOT NULL;

UPDATE instance_backup_runs
SET error_message_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.error_message)
WHERE error_message IS NOT NULL AND error_message != '';

-- Update references for instances
-- First ensure all values are in string_pool
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT name FROM instances WHERE name IS NOT NULL AND name != '';

UPDATE instances
SET name_id = (SELECT id FROM string_pool WHERE value = instances.name)
WHERE name IS NOT NULL AND name != '';

-- Update references for api_keys
-- First ensure all values are in string_pool
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT name FROM api_keys WHERE name IS NOT NULL AND name != '';

UPDATE api_keys
SET name_id = (SELECT id FROM string_pool WHERE value = api_keys.name)
WHERE name IS NOT NULL AND name != '';

-- Update references for client_api_keys  
-- First ensure all values are in string_pool
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT client_name FROM client_api_keys WHERE client_name IS NOT NULL AND client_name != '';

UPDATE client_api_keys
SET client_name_id = (SELECT id FROM string_pool WHERE value = client_api_keys.client_name)
WHERE client_name IS NOT NULL AND client_name != '';

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
    archive_path TEXT,
    manifest_path TEXT,
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
    FOREIGN KEY (error_message_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

INSERT INTO instance_backup_runs_new (id, instance_id, kind_id, status_id, requested_by_id, requested_at, started_at, completed_at, archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message_id)
SELECT id, instance_id, kind_id, status_id, requested_by_id, requested_at, started_at, completed_at, archive_path, manifest_path, total_bytes, torrent_count, category_counts_json, categories_json, tags_json, error_message_id
FROM instance_backup_runs;

DROP TABLE instance_backup_runs;
ALTER TABLE instance_backup_runs_new RENAME TO instance_backup_runs;

CREATE INDEX IF NOT EXISTS idx_instance_backup_runs_instance
    ON instance_backup_runs(instance_id, requested_at DESC);

-- Recreate instances table
CREATE TABLE IF NOT EXISTS instances_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name_id INTEGER NOT NULL,
    host TEXT NOT NULL,
    username TEXT NOT NULL,
    password_encrypted TEXT NOT NULL,
    basic_username TEXT,
    basic_password_encrypted TEXT,
    tls_skip_verify BOOLEAN NOT NULL DEFAULT 0,
    FOREIGN KEY (name_id) REFERENCES string_pool(id) ON DELETE RESTRICT
);

INSERT INTO instances_new (id, name_id, host, username, password_encrypted, basic_username, basic_password_encrypted, tls_skip_verify)
SELECT id, name_id, host, username, password_encrypted, basic_username, basic_password_encrypted, tls_skip_verify
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
FROM api_keys
WHERE name_id IS NOT NULL;

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
FROM client_api_keys
WHERE client_name_id IS NOT NULL;

DROP TABLE client_api_keys;
ALTER TABLE client_api_keys_new RENAME TO client_api_keys;

CREATE UNIQUE INDEX IF NOT EXISTS idx_client_api_keys_key_hash ON client_api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_client_api_keys_instance_id ON client_api_keys(instance_id);

-- Create view for api_keys that automatically joins with string_pool
CREATE VIEW IF NOT EXISTS api_keys_view AS
SELECT 
    ak.id,
    ak.key_hash,
    sp.value AS name,
    ak.created_at,
    ak.last_used_at
FROM api_keys ak
INNER JOIN string_pool sp ON ak.name_id = sp.id;

-- Create view for client_api_keys that automatically joins with string_pool
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

-- Create view for instance_backup_runs that automatically joins with string_pool
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
    ibr.archive_path,
    ibr.manifest_path,
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
LEFT JOIN string_pool sp_error ON ibr.error_message_id = sp_error.id;
