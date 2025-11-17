-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Combined Torznab + cross-seed schema migration (covers legacy migrations 013-023).
CREATE TABLE IF NOT EXISTS torznab_indexers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name_id INTEGER NOT NULL REFERENCES string_pool(id),
    base_url_id INTEGER NOT NULL REFERENCES string_pool(id),
    indexer_id_string_id INTEGER REFERENCES string_pool(id),
    api_key_encrypted TEXT NOT NULL,
    backend TEXT NOT NULL DEFAULT 'jackett' CHECK(backend IN ('jackett', 'prowlarr', 'native')),
    enabled BOOLEAN DEFAULT 1,
    priority INTEGER DEFAULT 0,
    timeout_seconds INTEGER DEFAULT 30,
    capabilities TEXT DEFAULT '[]',
    hourly_request_limit INTEGER,
    daily_request_limit INTEGER,
    last_test_at TIMESTAMP,
    last_test_status TEXT DEFAULT 'unknown' CHECK(last_test_status IN ('unknown', 'ok', 'error')),
    last_test_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_torznab_indexers_enabled ON torznab_indexers(enabled);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_priority ON torznab_indexers(priority DESC);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_name_id ON torznab_indexers(name_id);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_base_url_id ON torznab_indexers(base_url_id);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_indexer_id ON torznab_indexers(indexer_id_string_id);

CREATE TRIGGER IF NOT EXISTS update_torznab_indexers_updated_at
AFTER UPDATE ON torznab_indexers
BEGIN
    UPDATE torznab_indexers SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

DROP VIEW IF EXISTS torznab_indexers_view;
CREATE VIEW torznab_indexers_view AS
SELECT
    ti.id,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    sp_indexer_id.value AS indexer_id,
    ti.backend,
    ti.api_key_encrypted,
    ti.enabled,
    ti.priority,
    ti.timeout_seconds,
    ti.hourly_request_limit,
    ti.daily_request_limit,
    ti.last_test_at,
    ti.last_test_status,
    ti.last_test_error,
    ti.created_at,
    ti.updated_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id
LEFT JOIN string_pool sp_indexer_id ON ti.indexer_id_string_id = sp_indexer_id.id;

CREATE TABLE IF NOT EXISTS torznab_indexer_capabilities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    capability_type_id INTEGER NOT NULL REFERENCES string_pool(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(indexer_id, capability_type_id)
);

CREATE INDEX IF NOT EXISTS idx_torznab_capabilities_indexer ON torznab_indexer_capabilities(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_capabilities_type ON torznab_indexer_capabilities(capability_type_id);

CREATE TABLE IF NOT EXISTS torznab_indexer_categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    category_id INTEGER NOT NULL,
    category_name_id INTEGER NOT NULL REFERENCES string_pool(id),
    parent_category_id INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(indexer_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_torznab_categories_indexer ON torznab_indexer_categories(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_categories_category_id ON torznab_indexer_categories(category_id);
CREATE INDEX IF NOT EXISTS idx_torznab_categories_parent ON torznab_indexer_categories(parent_category_id);

CREATE TABLE IF NOT EXISTS torznab_indexer_errors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    error_message_id INTEGER NOT NULL REFERENCES string_pool(id),
    error_code TEXT,
    occurred_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP,
    error_count INTEGER DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_torznab_errors_indexer ON torznab_indexer_errors(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_errors_occurred ON torznab_indexer_errors(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_torznab_errors_unresolved ON torznab_indexer_errors(indexer_id, resolved_at) WHERE resolved_at IS NULL;

-- Persist rate-limit cooldown windows for Torznab indexers so restarts keep
-- the waiting period and avoid repeated tracker bans.
CREATE TABLE IF NOT EXISTS torznab_indexer_cooldowns (
    indexer_id INTEGER PRIMARY KEY REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    resume_at TIMESTAMP NOT NULL,
    cooldown_seconds INTEGER NOT NULL,
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_torznab_cooldowns_resume
    ON torznab_indexer_cooldowns(resume_at);

CREATE TRIGGER IF NOT EXISTS trg_torznab_cooldowns_updated_at
AFTER UPDATE ON torznab_indexer_cooldowns
FOR EACH ROW
BEGIN
    UPDATE torznab_indexer_cooldowns
    SET updated_at = CURRENT_TIMESTAMP
    WHERE indexer_id = NEW.indexer_id;
END;

CREATE TABLE IF NOT EXISTS torznab_indexer_latency (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    operation_type TEXT NOT NULL,
    latency_ms INTEGER NOT NULL,
    success BOOLEAN NOT NULL,
    measured_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_torznab_latency_indexer ON torznab_indexer_latency(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_latency_measured ON torznab_indexer_latency(measured_at DESC);
CREATE INDEX IF NOT EXISTS idx_torznab_latency_operation ON torznab_indexer_latency(indexer_id, operation_type, measured_at DESC);

DROP VIEW IF EXISTS torznab_indexer_latency_stats;
CREATE VIEW torznab_indexer_latency_stats AS
SELECT 
    indexer_id,
    operation_type,
    COUNT(*) AS total_requests,
    SUM(CASE WHEN success THEN 1 ELSE 0 END) AS successful_requests,
    AVG(CASE WHEN success THEN latency_ms ELSE NULL END) AS avg_latency_ms,
    MIN(CASE WHEN success THEN latency_ms ELSE NULL END) AS min_latency_ms,
    MAX(CASE WHEN success THEN latency_ms ELSE NULL END) AS max_latency_ms,
    CAST(SUM(CASE WHEN success THEN 1 ELSE 0 END) AS REAL) / COUNT(*) * 100 AS success_rate_pct,
    MAX(measured_at) AS last_measured_at
FROM torznab_indexer_latency
WHERE measured_at > datetime('now', '-7 days')
GROUP BY indexer_id, operation_type;

DROP VIEW IF EXISTS torznab_indexer_capabilities_view;
CREATE VIEW torznab_indexer_capabilities_view AS
SELECT 
    tic.indexer_id,
    sp.value AS capability_type
FROM torznab_indexer_capabilities tic
INNER JOIN string_pool sp ON tic.capability_type_id = sp.id;

DROP VIEW IF EXISTS torznab_indexer_categories_view;
CREATE VIEW torznab_indexer_categories_view AS
SELECT 
    tcat.indexer_id,
    tcat.category_id,
    sp.value AS category_name,
    tcat.parent_category_id
FROM torznab_indexer_categories tcat
INNER JOIN string_pool sp ON tcat.category_name_id = sp.id;

DROP VIEW IF EXISTS torznab_indexer_health;
CREATE VIEW torznab_indexer_health AS
SELECT
    ti.id AS indexer_id,
    sp_name.value AS indexer_name,
    ti.enabled,
    ti.last_test_status,
    COALESCE(err_recent.error_count, 0) AS errors_last_24h,
    COALESCE(err_unresolved.unresolved_count, 0) AS unresolved_errors,
    lat.avg_latency_ms,
    lat.success_rate_pct,
    lat.total_requests AS requests_last_7d,
    lat.last_measured_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
LEFT JOIN (
    SELECT indexer_id, COUNT(*) AS error_count
    FROM torznab_indexer_errors
    WHERE occurred_at > datetime('now', '-1 day')
    GROUP BY indexer_id
) err_recent ON ti.id = err_recent.indexer_id
LEFT JOIN (
    SELECT indexer_id, COUNT(*) AS unresolved_count
    FROM torznab_indexer_errors
    WHERE resolved_at IS NULL
    GROUP BY indexer_id
) err_unresolved ON ti.id = err_unresolved.indexer_id
LEFT JOIN (
    SELECT 
        indexer_id,
        AVG(CASE WHEN success THEN latency_ms ELSE NULL END) AS avg_latency_ms,
        CAST(SUM(CASE WHEN success THEN 1 ELSE 0 END) AS REAL) / COUNT(*) * 100 AS success_rate_pct,
        COUNT(*) AS total_requests,
        MAX(measured_at) AS last_measured_at
    FROM torznab_indexer_latency
    WHERE measured_at > datetime('now', '-7 days')
    GROUP BY indexer_id
) lat ON ti.id = lat.indexer_id;

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
    find_individual_episodes BOOLEAN NOT NULL DEFAULT 0,
    size_mismatch_tolerance_percent REAL NOT NULL DEFAULT 5.0,
    use_category_from_indexer BOOLEAN NOT NULL DEFAULT 0,
    run_external_program_id INTEGER,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (run_external_program_id) REFERENCES external_programs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_settings_external_program
    ON cross_seed_settings(run_external_program_id);

CREATE TRIGGER IF NOT EXISTS cross_seed_settings_updated_at
AFTER UPDATE ON cross_seed_settings
FOR EACH ROW
BEGIN
    UPDATE cross_seed_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = OLD.id;
END;

CREATE TABLE IF NOT EXISTS cross_seed_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    triggered_by TEXT NOT NULL,
    mode TEXT NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at DATETIME,
    total_feed_items INTEGER NOT NULL DEFAULT 0,
    candidates_found INTEGER NOT NULL DEFAULT 0,
    torrents_added INTEGER NOT NULL DEFAULT 0,
    torrents_failed INTEGER NOT NULL DEFAULT 0,
    torrents_skipped INTEGER NOT NULL DEFAULT 0,
    message TEXT,
    error_message TEXT,
    results_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_runs_started_at ON cross_seed_runs(started_at DESC);

CREATE TABLE IF NOT EXISTS cross_seed_feed_items (
    guid TEXT NOT NULL,
    indexer_id INTEGER NOT NULL,
    title TEXT,
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_status TEXT NOT NULL DEFAULT 'pending',
    last_run_id INTEGER,
    info_hash TEXT,
    PRIMARY KEY (guid, indexer_id),
    FOREIGN KEY (indexer_id) REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    FOREIGN KEY (last_run_id) REFERENCES cross_seed_runs(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_feed_items_indexer ON cross_seed_feed_items(indexer_id);
CREATE INDEX IF NOT EXISTS idx_cross_seed_feed_items_last_seen ON cross_seed_feed_items(last_seen_at DESC);

CREATE TRIGGER IF NOT EXISTS cross_seed_feed_items_touch
AFTER UPDATE ON cross_seed_feed_items
FOR EACH ROW
BEGIN
    UPDATE cross_seed_feed_items
    SET last_seen_at = CURRENT_TIMESTAMP
    WHERE guid = OLD.guid AND indexer_id = OLD.indexer_id;
END;

CREATE TABLE IF NOT EXISTS cross_seed_search_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    status TEXT NOT NULL,
    started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
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
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_search_runs_instance
    ON cross_seed_search_runs(instance_id, started_at DESC);

CREATE TABLE IF NOT EXISTS cross_seed_search_history (
    instance_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    last_searched_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, torrent_hash),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_search_history_last ON cross_seed_search_history(last_searched_at);

CREATE TABLE IF NOT EXISTS torznab_torrent_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    cache_key TEXT NOT NULL,
    guid TEXT,
    download_url TEXT,
    info_hash TEXT,
    title TEXT,
    size_bytes INTEGER,
    torrent_data BLOB NOT NULL,
    cached_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(indexer_id, cache_key)
);

CREATE INDEX IF NOT EXISTS idx_torznab_torrent_cache_last_used ON torznab_torrent_cache(last_used_at);
CREATE INDEX IF NOT EXISTS idx_torznab_torrent_cache_cache_key ON torznab_torrent_cache(indexer_id, cache_key);

CREATE TABLE IF NOT EXISTS torznab_search_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cache_key TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL DEFAULT 'generic',
    query TEXT,
    categories_json TEXT,
    indexer_ids_json TEXT,
    indexer_matcher TEXT NOT NULL DEFAULT '',
    request_fingerprint TEXT NOT NULL,
    response_data BLOB NOT NULL,
    total_results INTEGER NOT NULL DEFAULT 0,
    cached_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    hit_count INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_torznab_search_cache_scope ON torznab_search_cache(scope);
CREATE INDEX IF NOT EXISTS idx_torznab_search_cache_expires ON torznab_search_cache(expires_at);
CREATE INDEX IF NOT EXISTS idx_torznab_search_cache_matcher ON torznab_search_cache(indexer_matcher);

CREATE TABLE IF NOT EXISTS torznab_search_cache_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    ttl_minutes INTEGER NOT NULL DEFAULT 1440,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS torznab_search_cache_settings_updated_at
AFTER UPDATE ON torznab_search_cache_settings
FOR EACH ROW
BEGIN
    UPDATE torznab_search_cache_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

INSERT INTO torznab_search_cache_settings (id, ttl_minutes)
SELECT 1, 1440
WHERE NOT EXISTS (SELECT 1 FROM torznab_search_cache_settings WHERE id = 1);
