-- Add torznab_search_cache table for persisted Torznab search responses
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

-- Persist TTL configuration for the Torznab search cache.
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
