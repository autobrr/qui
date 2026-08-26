ALTER TABLE cross_seed_settings
    ADD COLUMN pooled_partial_completion_enabled INTEGER NOT NULL DEFAULT 0;

CREATE TABLE cross_seed_partial_pools (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_instance_id INTEGER NOT NULL,
    source_torrent_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'dormant')),
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (source_instance_id, source_torrent_key)
);

CREATE TABLE cross_seed_partial_pool_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pool_id INTEGER NOT NULL REFERENCES cross_seed_partial_pools(id) ON DELETE CASCADE,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    torrent_key TEXT NOT NULL,
    infohash_v1 TEXT NOT NULL DEFAULT '',
    infohash_v2 TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL CHECK (mode IN ('hardlink', 'reflink')),
    root_path TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'verifying', 'waiting', 'blocked', 'acquiring', 'rechecking',
        'complete', 'manual', 'removed'
    )),
    missing_bytes INTEGER NOT NULL DEFAULT 0,
    started_by_pool INTEGER NOT NULL DEFAULT 0,
    last_downloaded_bytes INTEGER,
    last_progress_at TIMESTAMP,
    retry_after TIMESTAMP,
    review_pause_pending INTEGER NOT NULL DEFAULT 0,
    resume_attempts INTEGER,
    recovery_attempts INTEGER,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_cross_seed_partial_pool_members_pool_status
    ON cross_seed_partial_pool_members(pool_id, status);
CREATE UNIQUE INDEX idx_cross_seed_partial_pool_members_instance_key
    ON cross_seed_partial_pool_members(instance_id, torrent_key)
    WHERE status <> 'removed';

CREATE TABLE cross_seed_partial_pool_member_files (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    member_id INTEGER NOT NULL REFERENCES cross_seed_partial_pool_members(id) ON DELETE CASCADE,
    file_index INTEGER NOT NULL,
    relative_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    pieces_root TEXT,
    wanted_at_admission INTEGER NOT NULL,
    materialized_at_add INTEGER NOT NULL,
    replaceable_at_add INTEGER NOT NULL,
    status TEXT NOT NULL CHECK (status IN (
        'present', 'missing', 'acquiring', 'available', 'propagating',
        'verifying', 'verified', 'manual'
    )),
    source_file_id INTEGER REFERENCES cross_seed_partial_pool_member_files(id) ON DELETE SET NULL,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (member_id, file_index)
);

CREATE INDEX idx_cross_seed_partial_pool_files_member_status
    ON cross_seed_partial_pool_member_files(member_id, status);
CREATE INDEX idx_cross_seed_partial_pool_files_source
    ON cross_seed_partial_pool_member_files(source_file_id);
