-- Drop the delete_unregistered_min_age column from automations.
-- This feature is redundant with the expression-based condition system
-- which can express age requirements using ADDED_ON or SEEDING_TIME fields.

-- SQLite doesn't support DROP COLUMN directly prior to 3.35.0,
-- but modernc.org/sqlite supports it. Use ALTER TABLE DROP COLUMN.
ALTER TABLE automations DROP COLUMN delete_unregistered_min_age;
