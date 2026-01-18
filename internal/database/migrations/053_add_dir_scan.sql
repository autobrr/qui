-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Directory Scanner feature: scans user-configured directories for media files,
-- searches Torznab indexers for matching torrents, and injects them into qBittorrent.

-- Global dir-scan settings
CREATE TABLE IF NOT EXISTS dir_scan_settings (
    id                                 INTEGER PRIMARY KEY CHECK(id = 1),
    enabled                            INTEGER NOT NULL DEFAULT 0,
    match_mode                         TEXT NOT NULL DEFAULT 'strict',
    size_tolerance_percent             REAL NOT NULL DEFAULT 5.0,
    min_piece_ratio                    REAL NOT NULL DEFAULT 0.98,
    allow_partial                      INTEGER NOT NULL DEFAULT 0,
    skip_piece_boundary_safety_check   INTEGER NOT NULL DEFAULT 1,
    start_paused                       INTEGER NOT NULL DEFAULT 1,
    category                           TEXT,
    tags                               TEXT,
    created_at                         DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at                         DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS trg_dir_scan_settings_updated
AFTER UPDATE ON dir_scan_settings
BEGIN
    UPDATE dir_scan_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- Insert default settings row
INSERT OR IGNORE INTO dir_scan_settings (id) VALUES (1);

-- Configured scan directories
CREATE TABLE IF NOT EXISTS dir_scan_directories (
    id                       INTEGER PRIMARY KEY AUTOINCREMENT,
    path                     TEXT NOT NULL,
    qbit_path_prefix         TEXT,
    enabled                  INTEGER NOT NULL DEFAULT 1,
    arr_instance_id          INTEGER REFERENCES arr_instances(id) ON DELETE SET NULL,
    target_instance_id       INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    scan_interval_minutes    INTEGER NOT NULL DEFAULT 1440,
    last_scan_at             DATETIME,
    category                 TEXT,
    tags                     TEXT,
    created_at               DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at               DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS trg_dir_scan_directories_updated
AFTER UPDATE ON dir_scan_directories
BEGIN
    UPDATE dir_scan_directories SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- Scan run history
CREATE TABLE IF NOT EXISTS dir_scan_runs (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    directory_id     INTEGER NOT NULL REFERENCES dir_scan_directories(id) ON DELETE CASCADE,
    status           TEXT NOT NULL,
    triggered_by     TEXT NOT NULL,
    files_found      INTEGER NOT NULL DEFAULT 0,
    files_skipped    INTEGER NOT NULL DEFAULT 0,
    matches_found    INTEGER NOT NULL DEFAULT 0,
    torrents_added   INTEGER NOT NULL DEFAULT 0,
    error_message    TEXT,
    started_at       DATETIME DEFAULT CURRENT_TIMESTAMP,
    completed_at     DATETIME
);

CREATE INDEX IF NOT EXISTS idx_dir_scan_runs_directory_started
    ON dir_scan_runs(directory_id, started_at DESC);

-- Scanned file tracking (avoid re-processing, handle renames via FileID)
CREATE TABLE IF NOT EXISTS dir_scan_files (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,
    directory_id         INTEGER NOT NULL REFERENCES dir_scan_directories(id) ON DELETE CASCADE,
    file_path            TEXT NOT NULL,
    file_size            INTEGER NOT NULL,
    file_mod_time        DATETIME NOT NULL,
    file_id              BLOB,
    status               TEXT NOT NULL DEFAULT 'pending',
    matched_torrent_hash TEXT,
    matched_indexer_id   INTEGER,
    last_processed_at    DATETIME,
    UNIQUE(directory_id, file_path)
);

CREATE INDEX IF NOT EXISTS idx_dir_scan_files_fileid
    ON dir_scan_files(directory_id, file_id)
    WHERE file_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_dir_scan_files_directory
    ON dir_scan_files(directory_id);

-- Store per-run injection attempts (successful and failed) for dir-scan runs.
-- This supports UI expansion of run rows to show what was added/failed.
CREATE TABLE IF NOT EXISTS dir_scan_run_injections (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id                INTEGER NOT NULL REFERENCES dir_scan_runs(id) ON DELETE CASCADE,
    directory_id          INTEGER NOT NULL REFERENCES dir_scan_directories(id) ON DELETE CASCADE,
    status                TEXT NOT NULL, -- added | failed
    searchee_name         TEXT NOT NULL,
    torrent_name          TEXT NOT NULL,
    info_hash             TEXT NOT NULL,
    content_type          TEXT NOT NULL, -- movie | tv
    indexer_name          TEXT,
    tracker_domain        TEXT,
    tracker_display_name  TEXT,
    link_mode             TEXT,
    save_path             TEXT,
    category              TEXT,
    tags                  TEXT, -- JSON array
    error_message         TEXT,
    created_at            DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_dir_scan_run_injections_run_created
    ON dir_scan_run_injections(run_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dir_scan_run_injections_directory_created
    ON dir_scan_run_injections(directory_id, created_at DESC);
