-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Create transfers table for tracking torrent moves between instances
CREATE TABLE IF NOT EXISTS transfers (
    id                   INTEGER PRIMARY KEY AUTOINCREMENT,

    -- Source and target
    source_instance_id   INTEGER NOT NULL,
    target_instance_id   INTEGER NOT NULL,
    torrent_hash         TEXT NOT NULL,
    torrent_name         TEXT NOT NULL,

    -- State machine
    state                TEXT NOT NULL DEFAULT 'pending',

    -- Persisted configuration for recovery
    source_save_path     TEXT,
    target_save_path     TEXT,
    link_mode            TEXT,  -- 'hardlink', 'reflink', 'direct'

    -- Options
    delete_from_source   INTEGER NOT NULL DEFAULT 1,
    preserve_category    INTEGER NOT NULL DEFAULT 1,
    preserve_tags        INTEGER NOT NULL DEFAULT 1,
    target_category      TEXT,
    target_tags          TEXT,  -- JSON array
    path_mappings        TEXT,  -- JSON object

    -- Progress tracking
    files_total          INTEGER NOT NULL DEFAULT 0,
    files_linked         INTEGER NOT NULL DEFAULT 0,

    -- Error info
    error                TEXT,

    -- Timestamps
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at         DATETIME,

    FOREIGN KEY (source_instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (target_instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_transfers_state ON transfers(state);
CREATE INDEX IF NOT EXISTS idx_transfers_source ON transfers(source_instance_id, state);
CREATE INDEX IF NOT EXISTS idx_transfers_target ON transfers(target_instance_id, state);
CREATE INDEX IF NOT EXISTS idx_transfers_hash ON transfers(torrent_hash);

CREATE TRIGGER IF NOT EXISTS trg_transfers_updated
AFTER UPDATE ON transfers
BEGIN
    UPDATE transfers
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
