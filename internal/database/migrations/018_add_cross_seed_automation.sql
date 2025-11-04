-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS cross_seed_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT 0,
    run_interval_minutes INTEGER NOT NULL DEFAULT 120,
    start_paused BOOLEAN NOT NULL DEFAULT 1,
    category TEXT,
    tags TEXT NOT NULL DEFAULT '[]',
    ignore_patterns TEXT NOT NULL DEFAULT '[]',
    target_instance_ids TEXT NOT NULL DEFAULT '[]',
    target_indexer_ids TEXT NOT NULL DEFAULT '[]',
    max_results_per_run INTEGER NOT NULL DEFAULT 50,
    created_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    updated_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);

CREATE TRIGGER IF NOT EXISTS cross_seed_settings_updated_at
AFTER UPDATE ON cross_seed_settings
FOR EACH ROW
BEGIN
    UPDATE cross_seed_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = OLD.id;
END;

CREATE TABLE IF NOT EXISTS cross_seed_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    triggered_by TEXT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    completed_at DATETIME,
    total_feed_items INTEGER NOT NULL DEFAULT 0,
    candidates_found INTEGER NOT NULL DEFAULT 0,
    torrents_added INTEGER NOT NULL DEFAULT 0,
    torrents_failed INTEGER NOT NULL DEFAULT 0,
    torrents_skipped INTEGER NOT NULL DEFAULT 0,
    message TEXT,
    error_message TEXT,
    results_json TEXT,
    created_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP)
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_runs_started_at
    ON cross_seed_runs (started_at DESC);

CREATE TABLE IF NOT EXISTS cross_seed_feed_items (
    guid TEXT NOT NULL,
    indexer_id INTEGER NOT NULL,
    title TEXT,
    first_seen_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    last_seen_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    last_status TEXT NOT NULL DEFAULT 'pending',
    last_run_id INTEGER,
    info_hash TEXT,
    PRIMARY KEY (guid, indexer_id),
    FOREIGN KEY (last_run_id) REFERENCES cross_seed_runs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_feed_items_last_seen
    ON cross_seed_feed_items (last_seen_at DESC);

CREATE TRIGGER IF NOT EXISTS cross_seed_feed_items_touch
AFTER UPDATE ON cross_seed_feed_items
FOR EACH ROW
BEGIN
    UPDATE cross_seed_feed_items
    SET last_seen_at = CURRENT_TIMESTAMP
WHERE guid = OLD.guid AND indexer_id = OLD.indexer_id;
END;
