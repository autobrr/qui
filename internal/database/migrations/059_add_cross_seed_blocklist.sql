-- Create per-instance cross-seed blocklist
CREATE TABLE IF NOT EXISTS cross_seed_blocklist (
    instance_id INTEGER NOT NULL,
    infohash TEXT NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, infohash),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_blocklist_instance
    ON cross_seed_blocklist(instance_id, created_at DESC);
