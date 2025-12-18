CREATE TABLE IF NOT EXISTS tracker_rule_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    hash TEXT NOT NULL,
    torrent_name TEXT,
    action TEXT NOT NULL,
    rule_id INTEGER,
    rule_name TEXT,
    outcome TEXT NOT NULL,
    reason TEXT,
    details TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tracker_rule_activity_instance_created
    ON tracker_rule_activity(instance_id, created_at DESC);
