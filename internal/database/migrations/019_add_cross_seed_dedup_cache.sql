-- Persist deduplicated torrent metadata to skip redundant qBittorrent lookups
CREATE TABLE IF NOT EXISTS cross_seed_dedup_cache (
    instance_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    representative_hash TEXT NOT NULL,
    has_top_level_folder BOOLEAN NOT NULL DEFAULT 0,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, torrent_hash),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_dedup_cache_rep
    ON cross_seed_dedup_cache(instance_id, representative_hash);

CREATE INDEX IF NOT EXISTS idx_cross_seed_dedup_cache_last_seen
    ON cross_seed_dedup_cache(instance_id, last_seen_at);
