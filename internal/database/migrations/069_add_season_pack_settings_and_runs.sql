-- Season pack settings on cross_seed_settings table.
ALTER TABLE cross_seed_settings ADD COLUMN season_pack_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN season_pack_coverage_threshold REAL NOT NULL DEFAULT 0.75;

-- Season pack run activity table.
CREATE TABLE season_pack_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    torrent_name TEXT NOT NULL,
    phase TEXT NOT NULL,
    status TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    instance_id INTEGER,
    matched_episodes INTEGER NOT NULL DEFAULT 0,
    total_episodes INTEGER NOT NULL DEFAULT 0,
    coverage REAL NOT NULL DEFAULT 0,
    link_mode TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
