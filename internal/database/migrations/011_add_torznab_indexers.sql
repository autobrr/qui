-- Add torznab_indexers table for supporting multiple Torznab/Jackett indexers
CREATE TABLE IF NOT EXISTS torznab_indexers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    base_url TEXT NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    enabled BOOLEAN DEFAULT 1,
    priority INTEGER DEFAULT 0,
    timeout_seconds INTEGER DEFAULT 30,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index for enabled lookups
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_enabled ON torznab_indexers(enabled);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_priority ON torznab_indexers(priority DESC);

-- Trigger to update the updated_at timestamp
CREATE TRIGGER IF NOT EXISTS update_torznab_indexers_updated_at 
AFTER UPDATE ON torznab_indexers
BEGIN
    UPDATE torznab_indexers SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
