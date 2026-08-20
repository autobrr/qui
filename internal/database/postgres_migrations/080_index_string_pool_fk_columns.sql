-- Index string_pool foreign-key child columns (Postgres mirror of SQLite migration 078).
--
-- Every table below references string_pool(id) via ON DELETE RESTRICT. Postgres does not
-- auto-create indexes on foreign-key child columns, so the daily string_pool garbage
-- collector (CleanupUnusedStrings) had to sequential-scan each unindexed column once per
-- deleted string to satisfy RESTRICT, eventually exceeding its deadline on large pools.
-- See discussion #2048.
--
-- FK enforcement probes `WHERE col = ?`, so the column must be the LEADING column of an
-- index; a composite index that merely contains it as a later column does not help. That
-- is why torrent_hash_id gets a dedicated index despite already being the second column of
-- idx_torrent_files_cache_lookup / the torrent_files_sync primary key.

-- torrent_files_cache (large)
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_name_id ON torrent_files_cache(name_id);
CREATE INDEX IF NOT EXISTS idx_torrent_files_cache_hash_id ON torrent_files_cache(torrent_hash_id);

-- torrent_files_sync (large)
CREATE INDEX IF NOT EXISTS idx_torrent_files_sync_hash_id ON torrent_files_sync(torrent_hash_id);

-- instance_backup_items (large): torrent_hash_id already covered by idx_backup_items_hash
CREATE INDEX IF NOT EXISTS idx_backup_items_name_id ON instance_backup_items(name_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_category_id ON instance_backup_items(category_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_tags_id ON instance_backup_items(tags_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_archive_rel_path_id ON instance_backup_items(archive_rel_path_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_infohash_v1_id ON instance_backup_items(infohash_v1_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_infohash_v2_id ON instance_backup_items(infohash_v2_id);
CREATE INDEX IF NOT EXISTS idx_backup_items_torrent_blob_path_id ON instance_backup_items(torrent_blob_path_id);

-- instance_backup_runs (grows with backup history)
CREATE INDEX IF NOT EXISTS idx_backup_runs_kind_id ON instance_backup_runs(kind_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_status_id ON instance_backup_runs(status_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_requested_by_id ON instance_backup_runs(requested_by_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_error_message_id ON instance_backup_runs(error_message_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_archive_path_id ON instance_backup_runs(archive_path_id);
CREATE INDEX IF NOT EXISTS idx_backup_runs_manifest_path_id ON instance_backup_runs(manifest_path_id);

-- bounded tables: cheap indexes that keep the invariant "every string_pool FK column has a leading index"
CREATE INDEX IF NOT EXISTS idx_instances_name_id ON instances(name_id);
CREATE INDEX IF NOT EXISTS idx_instances_host_id ON instances(host_id);
CREATE INDEX IF NOT EXISTS idx_instances_username_id ON instances(username_id);
CREATE INDEX IF NOT EXISTS idx_instances_basic_username_id ON instances(basic_username_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_name_id ON api_keys(name_id);
CREATE INDEX IF NOT EXISTS idx_client_api_keys_client_name_id ON client_api_keys(client_name_id);
CREATE INDEX IF NOT EXISTS idx_instance_errors_error_type_id ON instance_errors(error_type_id);
CREATE INDEX IF NOT EXISTS idx_instance_errors_error_message_id ON instance_errors(error_message_id);
CREATE INDEX IF NOT EXISTS idx_torznab_indexers_basic_username_id ON torznab_indexers(basic_username_id);
CREATE INDEX IF NOT EXISTS idx_torznab_categories_category_name_id ON torznab_indexer_categories(category_name_id);
CREATE INDEX IF NOT EXISTS idx_torznab_errors_error_message_id ON torznab_indexer_errors(error_message_id);
CREATE INDEX IF NOT EXISTS idx_arr_instances_basic_username_id ON arr_instances(basic_username_id);
