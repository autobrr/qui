-- Cache torrent file information to reduce API calls to qBittorrent
-- Files for 100% complete torrents are assumed stable and don't need frequent refreshing
CREATE TABLE IF NOT EXISTS torrent_files_cache (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    file_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    size INTEGER NOT NULL,
    progress REAL NOT NULL,
    priority INTEGER NOT NULL,
    is_seed INTEGER,
    piece_range_start INTEGER,
    piece_range_end INTEGER,
    availability REAL NOT NULL,
    cached_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    UNIQUE(instance_id, torrent_hash, file_index)
);

-- Index for fast lookups by instance and torrent hash
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_lookup ON torrent_files_cache(instance_id, torrent_hash);

-- Index for cache invalidation queries
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_cached_at ON torrent_files_cache(cached_at);

-- Store metadata about when each torrent's files were last synced
CREATE TABLE IF NOT EXISTS torrent_files_sync (
    instance_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    last_synced_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    torrent_progress REAL NOT NULL DEFAULT 0,
    file_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (instance_id, torrent_hash),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

-- Index for finding stale caches
CREATE INDEX IF NOT EXISTS idx_torrent_files_sync_last_synced ON torrent_files_sync(last_synced_at);
