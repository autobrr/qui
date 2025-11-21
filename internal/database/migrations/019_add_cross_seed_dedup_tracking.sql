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

-- Track hashes previously added via cross-seed so we can prevent re-adding identical torrents
ALTER TABLE cross_seed_settings
    ADD COLUMN prevent_readd_hashes BOOLEAN NOT NULL DEFAULT 1;

CREATE TABLE IF NOT EXISTS cross_seed_previously_added_hashes (
    normalized_hash TEXT NOT NULL PRIMARY KEY,
    original_hash   TEXT NOT NULL,
    first_instance_id INTEGER,
    last_instance_id  INTEGER,
    first_added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_added_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(first_instance_id) REFERENCES instances(id) ON DELETE SET NULL,
    FOREIGN KEY(last_instance_id)  REFERENCES instances(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_previously_added_last_at
    ON cross_seed_previously_added_hashes(last_added_at);
