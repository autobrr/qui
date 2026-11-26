-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- ARR (Sonarr/Radarr) instance configuration for ID-driven searching
CREATE TABLE IF NOT EXISTS arr_instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    type TEXT NOT NULL CHECK(type IN ('sonarr', 'radarr')),
    name_id INTEGER NOT NULL REFERENCES string_pool(id),
    base_url_id INTEGER NOT NULL REFERENCES string_pool(id),
    api_key_encrypted TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    priority INTEGER DEFAULT 0,
    timeout_seconds INTEGER DEFAULT 15,
    last_test_at TIMESTAMP,
    last_test_status TEXT DEFAULT 'unknown' CHECK(last_test_status IN ('unknown', 'ok', 'error')),
    last_test_error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_arr_instances_enabled ON arr_instances(enabled);
CREATE INDEX IF NOT EXISTS idx_arr_instances_type ON arr_instances(type);
CREATE INDEX IF NOT EXISTS idx_arr_instances_priority ON arr_instances(type, enabled, priority DESC);
CREATE INDEX IF NOT EXISTS idx_arr_instances_name_id ON arr_instances(name_id);
CREATE INDEX IF NOT EXISTS idx_arr_instances_base_url_id ON arr_instances(base_url_id);

-- Prevent duplicate URLs for the same ARR type
CREATE UNIQUE INDEX IF NOT EXISTS idx_arr_instances_type_base_url ON arr_instances(type, base_url_id);

CREATE TRIGGER IF NOT EXISTS update_arr_instances_updated_at
AFTER UPDATE ON arr_instances
BEGIN
    UPDATE arr_instances SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

DROP VIEW IF EXISTS arr_instances_view;
CREATE VIEW arr_instances_view AS
SELECT
    ai.id,
    ai.type,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    ai.api_key_encrypted,
    ai.enabled,
    ai.priority,
    ai.timeout_seconds,
    ai.last_test_at,
    ai.last_test_status,
    ai.last_test_error,
    ai.created_at,
    ai.updated_at
FROM arr_instances ai
INNER JOIN string_pool sp_name ON ai.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ai.base_url_id = sp_base_url.id;

-- ARR ID cache for storing resolved external IDs with TTL and negative caching
CREATE TABLE IF NOT EXISTS arr_id_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    title_hash TEXT NOT NULL,
    content_type TEXT NOT NULL CHECK(content_type IN ('movie', 'tv', 'anime', 'unknown')),
    arr_instance_id INTEGER REFERENCES arr_instances(id) ON DELETE SET NULL,
    imdb_id TEXT,
    tmdb_id INTEGER,
    tvdb_id INTEGER,
    tvmaze_id INTEGER,
    is_negative BOOLEAN DEFAULT 0,
    cached_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP NOT NULL,
    UNIQUE(title_hash, content_type)
);

CREATE INDEX IF NOT EXISTS idx_arr_id_cache_lookup ON arr_id_cache(title_hash, content_type);
CREATE INDEX IF NOT EXISTS idx_arr_id_cache_expires ON arr_id_cache(expires_at);
CREATE INDEX IF NOT EXISTS idx_arr_id_cache_instance ON arr_id_cache(arr_instance_id);
