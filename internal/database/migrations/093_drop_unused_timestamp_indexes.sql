-- Migration 093: Drop unused timestamp indexes on torrent_files_cache/torrent_files_sync
--
-- cached_at and last_synced_at are rewritten on every upsert (once per torrent per sync
-- cycle), but neither column is ever used in a WHERE or ORDER BY clause: cache eviction
-- deletes by (instance_id, torrent_hash_id) identity, and the only reads of these columns
-- are unfiltered aggregates (MIN/MAX/AVG) that a seq scan already has to do. On Postgres
-- this indexed-but-unfiltered timestamp blocks HOT updates on every write, forcing full
-- index maintenance for no read benefit. See discussion #2374.

DROP INDEX IF EXISTS idx_torrent_files_cache_cached_at;
DROP INDEX IF EXISTS idx_torrent_files_sync_last_synced;
