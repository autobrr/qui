ALTER TABLE cross_seed_settings ADD COLUMN enable_pooled_partial_completion BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN allow_reflink_single_file_size_mismatch BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN max_missing_bytes_after_recheck INTEGER NOT NULL DEFAULT 104857600;

CREATE TABLE IF NOT EXISTS cross_seed_partial_pool_members (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source_instance_id INTEGER NOT NULL,
    source_hash TEXT NOT NULL,
    target_instance_id INTEGER NOT NULL,
    target_hash TEXT NOT NULL,
    target_hash_v2 TEXT,
    target_added_on INTEGER NOT NULL DEFAULT 0,
    target_name TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    managed_root TEXT NOT NULL,
    source_piece_length INTEGER NOT NULL DEFAULT 0,
    max_missing_bytes_after_recheck INTEGER NOT NULL DEFAULT 104857600,
    source_files_json TEXT NOT NULL DEFAULT '[]',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at DATETIME NOT NULL,
    UNIQUE(target_instance_id, target_hash)
);

CREATE INDEX IF NOT EXISTS idx_cross_seed_partial_pool_members_source
    ON cross_seed_partial_pool_members(source_instance_id, source_hash);

CREATE INDEX IF NOT EXISTS idx_cross_seed_partial_pool_members_expires
    ON cross_seed_partial_pool_members(expires_at);

CREATE TRIGGER IF NOT EXISTS trg_cross_seed_partial_pool_members_updated
AFTER UPDATE ON cross_seed_partial_pool_members
FOR EACH ROW
BEGIN
    UPDATE cross_seed_partial_pool_members
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
