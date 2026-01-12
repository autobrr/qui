-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

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

