-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Cross-seed search automation tables
CREATE TABLE IF NOT EXISTS cross_seed_search_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    completed_at DATETIME,
    total_torrents INTEGER NOT NULL DEFAULT 0,
    processed INTEGER NOT NULL DEFAULT 0,
    torrents_added INTEGER NOT NULL DEFAULT 0,
    torrents_failed INTEGER NOT NULL DEFAULT 0,
    torrents_skipped INTEGER NOT NULL DEFAULT 0,
    message TEXT,
    error_message TEXT,
    filters_json TEXT,
    indexer_ids_json TEXT,
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    cooldown_minutes INTEGER NOT NULL DEFAULT 360,
    results_json TEXT,
    created_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_search_runs_instance
    ON cross_seed_search_runs (instance_id, started_at DESC);

-- Track recently searched torrents per instance to avoid redundant work
CREATE TABLE IF NOT EXISTS cross_seed_search_history (
    instance_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    last_searched_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    PRIMARY KEY (instance_id, torrent_hash),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_search_history_last
    ON cross_seed_search_history (last_searched_at);

-- Cache downloaded Torznab torrent files for reuse across automation/search flows
CREATE TABLE IF NOT EXISTS torznab_torrent_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL,
    cache_key TEXT NOT NULL,
    guid TEXT,
    download_url TEXT,
    info_hash TEXT,
    title TEXT,
    size_bytes INTEGER,
    torrent_data BLOB NOT NULL,
    cached_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    last_used_at DATETIME NOT NULL DEFAULT (CURRENT_TIMESTAMP),
    FOREIGN KEY (indexer_id) REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    UNIQUE(indexer_id, cache_key)
);

CREATE INDEX IF NOT EXISTS idx_torznab_torrent_cache_last_used
    ON torznab_torrent_cache (last_used_at);

CREATE INDEX IF NOT EXISTS idx_torznab_torrent_cache_cache_key
    ON torznab_torrent_cache (indexer_id, cache_key);
