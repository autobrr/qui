-- Backup schedules and run history per qBittorrent instance
CREATE TABLE IF NOT EXISTS instance_backup_settings (
    instance_id INTEGER PRIMARY KEY,
    enabled BOOLEAN NOT NULL DEFAULT 0,
    hourly_enabled BOOLEAN NOT NULL DEFAULT 0,
    daily_enabled BOOLEAN NOT NULL DEFAULT 0,
    weekly_enabled BOOLEAN NOT NULL DEFAULT 0,
    monthly_enabled BOOLEAN NOT NULL DEFAULT 0,
    keep_last INTEGER NOT NULL DEFAULT 3,
    keep_hourly INTEGER NOT NULL DEFAULT 0,
    keep_daily INTEGER NOT NULL DEFAULT 7,
    keep_weekly INTEGER NOT NULL DEFAULT 4,
    keep_monthly INTEGER NOT NULL DEFAULT 12,
    include_categories BOOLEAN NOT NULL DEFAULT 1,
    include_tags BOOLEAN NOT NULL DEFAULT 1,
    custom_path TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE TRIGGER IF NOT EXISTS update_instance_backup_settings_updated_at
AFTER UPDATE ON instance_backup_settings
BEGIN
    UPDATE instance_backup_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE instance_id = NEW.instance_id;
END;

CREATE TABLE IF NOT EXISTS instance_backup_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    kind TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    requested_by TEXT NOT NULL DEFAULT 'system',
    requested_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    archive_path TEXT,
    manifest_path TEXT,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    torrent_count INTEGER NOT NULL DEFAULT 0,
    category_counts_json TEXT,
    error_message TEXT,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_instance_backup_runs_instance
    ON instance_backup_runs(instance_id, requested_at DESC);
CREATE INDEX IF NOT EXISTS idx_instance_backup_runs_status
    ON instance_backup_runs(status);
CREATE INDEX IF NOT EXISTS idx_instance_backup_runs_kind
    ON instance_backup_runs(instance_id, kind, requested_at DESC);

CREATE TABLE IF NOT EXISTS instance_backup_items (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    category TEXT,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    archive_rel_path TEXT,
    infohash_v1 TEXT,
    infohash_v2 TEXT,
    tags TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_id) REFERENCES instance_backup_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_instance_backup_items_run
    ON instance_backup_items(run_id);
CREATE INDEX IF NOT EXISTS idx_instance_backup_items_hash
    ON instance_backup_items(run_id, torrent_hash);
