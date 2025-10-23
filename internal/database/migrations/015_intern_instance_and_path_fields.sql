-- Migration 015: Intern remaining repetitive strings in instances and instance_backup_runs
-- Intern: host, username, basic_username, archive_path, manifest_path

-- Add temporary columns for string references
ALTER TABLE instances ADD COLUMN host_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instances ADD COLUMN username_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instances ADD COLUMN basic_username_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN archive_path_id INTEGER REFERENCES string_pool(id);
ALTER TABLE instance_backup_runs ADD COLUMN manifest_path_id INTEGER REFERENCES string_pool(id);

-- Populate string_pool with unique values from instances
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT host FROM instances WHERE host IS NOT NULL AND host != ''
UNION
SELECT DISTINCT username FROM instances WHERE username IS NOT NULL AND username != ''
UNION
SELECT DISTINCT basic_username FROM instances WHERE basic_username IS NOT NULL AND basic_username != '';

-- Populate string_pool with unique values from instance_backup_runs
INSERT OR IGNORE INTO string_pool (value)
SELECT DISTINCT archive_path FROM instance_backup_runs WHERE archive_path IS NOT NULL AND archive_path != ''
UNION
SELECT DISTINCT manifest_path FROM instance_backup_runs WHERE manifest_path IS NOT NULL AND manifest_path != '';

-- Update references for instances
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

-- Update references for instance_backup_runs
UPDATE instance_backup_runs
SET archive_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.archive_path)
WHERE archive_path IS NOT NULL AND archive_path != '';

UPDATE instance_backup_runs
SET manifest_path_id = (SELECT id FROM string_pool WHERE value = instance_backup_runs.manifest_path)
WHERE manifest_path IS NOT NULL AND manifest_path != '';

-- Drop existing views before recreating tables
DROP VIEW IF EXISTS instances_view;
DROP VIEW IF EXISTS instance_backup_runs_view;

-- Recreate instances table with interned strings
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

-- Recreate instance_backup_runs table with interned strings
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

-- Recreate index
CREATE INDEX IF NOT EXISTS idx_instance_backup_runs_instance
    ON instance_backup_runs(instance_id, requested_at DESC);

-- Recreate views with new interned fields
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
