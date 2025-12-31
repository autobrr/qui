-- Log exclusions: stores muted log message patterns
CREATE TABLE IF NOT EXISTS log_exclusions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    patterns TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS trg_log_exclusions_updated
AFTER UPDATE ON log_exclusions
BEGIN
    UPDATE log_exclusions SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
