-- Add torznab_indexers table for supporting multiple Torznab/Jackett indexers
-- Uses string_pool for name and base_url deduplication
CREATE TABLE IF NOT EXISTS torznab_indexers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name_id INTEGER NOT NULL REFERENCES string_pool(id),
    base_url_id INTEGER NOT NULL REFERENCES string_pool(id),
    api_key_encrypted TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    priority INTEGER DEFAULT 0,
    timeout_seconds INTEGER DEFAULT 30,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_enabled ON torznab_indexers(enabled);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_priority ON torznab_indexers(priority DESC);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_name_id ON torznab_indexers(name_id);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_base_url_id ON torznab_indexers(base_url_id);

-- Trigger to update the updated_at timestamp
CREATE TRIGGER IF NOT EXISTS update_torznab_indexers_updated_at 
AFTER UPDATE ON torznab_indexers
BEGIN
    UPDATE torznab_indexers SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

-- Create a view that joins with string_pool to provide convenient access
CREATE VIEW IF NOT EXISTS torznab_indexers_view AS
SELECT 
    ti.id,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    ti.api_key_encrypted,
    ti.enabled,
    ti.priority,
    ti.timeout_seconds,
    ti.created_at,
    ti.updated_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id;
