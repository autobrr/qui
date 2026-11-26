-- Persist seeded torrent search defaults
CREATE TABLE IF NOT EXISTS cross_seed_search_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    instance_id INTEGER,
    categories TEXT NOT NULL DEFAULT '[]',
    tags TEXT NOT NULL DEFAULT '[]',
    indexer_ids TEXT NOT NULL DEFAULT '[]',
    interval_seconds INTEGER NOT NULL DEFAULT 60,
    cooldown_minutes INTEGER NOT NULL DEFAULT 720,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE SET NULL
);

CREATE TRIGGER IF NOT EXISTS cross_seed_search_settings_updated_at
AFTER UPDATE ON cross_seed_search_settings
FOR EACH ROW
BEGIN
    UPDATE cross_seed_search_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
