-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Disc scans: one queued or running BDInfo job on one Disc, plus its cached report.
CREATE TABLE IF NOT EXISTS disc_scan_runs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id     INTEGER NOT NULL,
    torrent_hash    TEXT NOT NULL,
    disc_path       TEXT NOT NULL,
    resolved_path   TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    error_message   TEXT,
    processed_bytes INTEGER NOT NULL DEFAULT 0,
    total_bytes     INTEGER NOT NULL DEFAULT 0,
    report          TEXT,
    quick_summary   TEXT,
    forums_block    TEXT,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    started_at      DATETIME,
    completed_at    DATETIME,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_disc_scan_runs_instance_resolved_path
    ON disc_scan_runs(instance_id, resolved_path, id DESC);

CREATE INDEX IF NOT EXISTS idx_disc_scan_runs_instance_torrent
    ON disc_scan_runs(instance_id, torrent_hash);
