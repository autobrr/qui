-- Refactor torznab indexer data into separate tables for better performance and normalization

-- Create table for indexer capabilities
CREATE TABLE IF NOT EXISTS torznab_indexer_capabilities (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    capability_type_id INTEGER NOT NULL REFERENCES string_pool(id), -- e.g., 'tv-search', 'movie-search', 'search'
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(indexer_id, capability_type_id)
);

CREATE INDEX IF NOT EXISTS idx_torznab_capabilities_indexer ON torznab_indexer_capabilities(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_capabilities_type ON torznab_indexer_capabilities(capability_type_id);

-- Create table for indexer categories (from caps response)
CREATE TABLE IF NOT EXISTS torznab_indexer_categories (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    category_id INTEGER NOT NULL, -- Torznab category ID (e.g., 5000 for TV, 2000 for Movies)
    category_name_id INTEGER NOT NULL REFERENCES string_pool(id), -- Category name (e.g., 'TV', 'Movies')
    parent_category_id INTEGER, -- For subcategories (e.g., 5030 'TV/HD' parent is 5000)
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(indexer_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_torznab_categories_indexer ON torznab_indexer_categories(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_categories_category_id ON torznab_indexer_categories(category_id);
CREATE INDEX IF NOT EXISTS idx_torznab_categories_parent ON torznab_indexer_categories(parent_category_id);

-- Create table for indexer errors with history
CREATE TABLE IF NOT EXISTS torznab_indexer_errors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    error_message_id INTEGER NOT NULL REFERENCES string_pool(id),
    error_code TEXT, -- HTTP status code, timeout, connection error, etc.
    occurred_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMP, -- NULL if not resolved
    error_count INTEGER DEFAULT 1 -- Incremented for consecutive same errors
);

CREATE INDEX IF NOT EXISTS idx_torznab_errors_indexer ON torznab_indexer_errors(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_errors_occurred ON torznab_indexer_errors(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_torznab_errors_unresolved ON torznab_indexer_errors(indexer_id, resolved_at) WHERE resolved_at IS NULL;

-- Create table for latency statistics (rolling window)
CREATE TABLE IF NOT EXISTS torznab_indexer_latency (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    indexer_id INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    operation_type TEXT NOT NULL, -- 'search', 'caps', 'download', etc.
    latency_ms INTEGER NOT NULL, -- Response time in milliseconds
    success BOOLEAN NOT NULL, -- Whether the request succeeded
    measured_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_torznab_latency_indexer ON torznab_indexer_latency(indexer_id);
CREATE INDEX IF NOT EXISTS idx_torznab_latency_measured ON torznab_indexer_latency(measured_at DESC);
CREATE INDEX IF NOT EXISTS idx_torznab_latency_operation ON torznab_indexer_latency(indexer_id, operation_type, measured_at DESC);

-- Create aggregated latency statistics view for quick access
CREATE VIEW IF NOT EXISTS torznab_indexer_latency_stats AS
SELECT 
    indexer_id,
    operation_type,
    COUNT(*) as total_requests,
    SUM(CASE WHEN success THEN 1 ELSE 0 END) as successful_requests,
    AVG(CASE WHEN success THEN latency_ms ELSE NULL END) as avg_latency_ms,
    MIN(CASE WHEN success THEN latency_ms ELSE NULL END) as min_latency_ms,
    MAX(CASE WHEN success THEN latency_ms ELSE NULL END) as max_latency_ms,
    CAST(SUM(CASE WHEN success THEN 1 ELSE 0 END) AS REAL) / COUNT(*) * 100 as success_rate_pct,
    MAX(measured_at) as last_measured_at
FROM torznab_indexer_latency
WHERE measured_at > datetime('now', '-7 days') -- Rolling 7-day window
GROUP BY indexer_id, operation_type;

-- Note: Migration of existing capabilities and last_test_error will be handled in application code
-- The old 'capabilities' column from migration 013 is kept for backward compatibility
-- but the application will now use the new torznab_indexer_capabilities table instead

-- Update the main view to use the new structure (simple version, aggregate in application code)
DROP VIEW IF EXISTS torznab_indexers_view;
CREATE VIEW torznab_indexers_view AS
SELECT 
    ti.id,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    ti.api_key_encrypted,
    ti.enabled,
    ti.priority,
    ti.timeout_seconds,
    ti.last_test_at,
    ti.last_test_status,
    ti.last_test_error,
    ti.created_at,
    ti.updated_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id;

-- Simple views for capabilities and categories (join in application code)
CREATE VIEW IF NOT EXISTS torznab_indexer_capabilities_view AS
SELECT 
    tic.indexer_id,
    sp.value as capability_type
FROM torznab_indexer_capabilities tic
INNER JOIN string_pool sp ON tic.capability_type_id = sp.id;

CREATE VIEW IF NOT EXISTS torznab_indexer_categories_view AS
SELECT 
    tcat.indexer_id,
    tcat.category_id,
    sp.value as category_name,
    tcat.parent_category_id
FROM torznab_indexer_categories tcat
INNER JOIN string_pool sp ON tcat.category_name_id = sp.id;

-- Create view for indexer health summary
CREATE VIEW IF NOT EXISTS torznab_indexer_health AS
SELECT
    ti.id as indexer_id,
    sp_name.value as indexer_name,
    ti.enabled,
    ti.last_test_status,
    -- Error metrics
    COALESCE(err_recent.error_count, 0) as errors_last_24h,
    COALESCE(err_unresolved.unresolved_count, 0) as unresolved_errors,
    -- Latency metrics (last 7 days)
    lat.avg_latency_ms,
    lat.success_rate_pct,
    lat.total_requests as requests_last_7d,
    lat.last_measured_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
LEFT JOIN (
    SELECT indexer_id, COUNT(*) as error_count
    FROM torznab_indexer_errors
    WHERE occurred_at > datetime('now', '-1 day')
    GROUP BY indexer_id
) err_recent ON ti.id = err_recent.indexer_id
LEFT JOIN (
    SELECT indexer_id, COUNT(*) as unresolved_count
    FROM torznab_indexer_errors
    WHERE resolved_at IS NULL
    GROUP BY indexer_id
) err_unresolved ON ti.id = err_unresolved.indexer_id
LEFT JOIN (
    SELECT 
        indexer_id,
        AVG(CASE WHEN success THEN latency_ms ELSE NULL END) as avg_latency_ms,
        CAST(SUM(CASE WHEN success THEN 1 ELSE 0 END) AS REAL) / COUNT(*) * 100 as success_rate_pct,
        COUNT(*) as total_requests,
        MAX(measured_at) as last_measured_at
    FROM torznab_indexer_latency
    WHERE measured_at > datetime('now', '-7 days')
    GROUP BY indexer_id
) lat ON ti.id = lat.indexer_id;
