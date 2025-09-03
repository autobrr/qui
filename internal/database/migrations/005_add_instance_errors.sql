-- Instance error tracking table for storing the last 5 unique errors per instance
CREATE TABLE IF NOT EXISTS instance_errors (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    error_type TEXT NOT NULL,
    error_message TEXT NOT NULL,
    occurred_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

-- Index for efficient error lookups ordered by most recent
CREATE INDEX IF NOT EXISTS idx_instance_errors_lookup 
ON instance_errors(instance_id, occurred_at DESC);

-- Auto-cleanup trigger to keep only the last 5 errors per instance
CREATE TRIGGER IF NOT EXISTS cleanup_old_instance_errors
AFTER INSERT ON instance_errors
BEGIN
    DELETE FROM instance_errors
    WHERE instance_id = NEW.instance_id
    AND id NOT IN (
        SELECT id FROM instance_errors
        WHERE instance_id = NEW.instance_id
        ORDER BY occurred_at DESC
        LIMIT 5
    );
END;