-- Drop unused timestamp indexes on torrent_files_cache/torrent_files_sync (Postgres
-- mirror of SQLite migration 093).
--
-- cached_at and last_synced_at are rewritten on every upsert (once per torrent per sync
-- cycle), but neither column is ever used in a WHERE or ORDER BY clause: cache eviction
-- deletes by (instance_id, torrent_hash_id) identity, and the only reads of these columns
-- are unfiltered aggregates (MIN/MAX/AVG) that a seq scan already has to do anyway. With
-- the index in place, Postgres cannot use HOT updates for these high-churn tables because
-- the indexed timestamp changes on every write, forcing full index maintenance for no read
-- benefit. Plain DROP INDEX (not CONCURRENTLY) is fine here: dropping is a fast metadata
-- change and migrations already run inside a transaction, which disallows CONCURRENTLY.
-- See discussion #2374.

DROP INDEX IF EXISTS idx_torrent_files_cache_cached_at;
DROP INDEX IF EXISTS idx_torrent_files_sync_last_synced;
