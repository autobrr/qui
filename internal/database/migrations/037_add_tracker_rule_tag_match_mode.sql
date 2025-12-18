-- +migrate Up
ALTER TABLE tracker_rules ADD COLUMN tag_match_mode TEXT DEFAULT 'any';

-- +migrate Down
-- SQLite does not support DROP COLUMN directly
