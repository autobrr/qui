-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Per-instance orphan scan settings
CREATE TABLE IF NOT EXISTS orphan_scan_settings (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id           INTEGER NOT NULL UNIQUE,
    enabled               INTEGER NOT NULL DEFAULT 0,
    grace_period_minutes  INTEGER NOT NULL DEFAULT 10,
    ignore_paths          TEXT,
    scan_interval_hours   INTEGER NOT NULL DEFAULT 24,
    max_files_per_run     INTEGER NOT NULL DEFAULT 10000,
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at            DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE TRIGGER IF NOT EXISTS trg_orphan_scan_settings_updated
AFTER UPDATE ON orphan_scan_settings
BEGIN
    UPDATE orphan_scan_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- Scan run history
CREATE TABLE IF NOT EXISTS orphan_scan_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id     INTEGER NOT NULL,
    status          TEXT NOT NULL,
    triggered_by    TEXT NOT NULL,
    scan_paths      TEXT,
    files_found     INTEGER DEFAULT 0,
    files_deleted   INTEGER DEFAULT 0,
    folders_deleted INTEGER DEFAULT 0,
    bytes_reclaimed INTEGER DEFAULT 0,
    truncated       INTEGER NOT NULL DEFAULT 0,
    error_message   TEXT,
    started_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at    DATETIME,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_orphan_scan_runs_instance_started
    ON orphan_scan_runs(instance_id, started_at DESC);

-- Orphan files found in a scan (for preview)
CREATE TABLE IF NOT EXISTS orphan_scan_files (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id        INTEGER NOT NULL,
    file_path     TEXT NOT NULL,
    file_size     INTEGER NOT NULL,
    modified_at   DATETIME,
    status        TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    FOREIGN KEY (run_id) REFERENCES orphan_scan_runs(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_orphan_scan_files_run ON orphan_scan_files(run_id);
