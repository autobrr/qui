-- Migration 092: Drop the redundant cross_seed_feed_items_touch trigger (SQLite only)
--
-- This AFTER UPDATE trigger unconditionally overwrites last_seen_at with
-- CURRENT_TIMESTAMP on every update, duplicating what MarkFeedItem's own
-- ON CONFLICT ... DO UPDATE SET last_seen_at = excluded.last_seen_at already
-- does. Beyond being a wasted extra UPDATE per write, it silently discards
-- whatever value the application computed for last_seen_at (see discussion
-- #2375, where MarkFeedItem now truncates last_seen_at to day precision so
-- same-day polls write an identical value; this trigger overrode that with a
-- fresh wall-clock timestamp every time). Postgres never had this trigger, so
-- there is no Postgres mirror for this migration.

DROP TRIGGER IF EXISTS cross_seed_feed_items_touch;
