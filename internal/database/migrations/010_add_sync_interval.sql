-- Add sync_interval column to instances table
-- sync_interval: Number of minutes between automatic sync updates (0 = disabled, minimum 5 minutes)
ALTER TABLE instances ADD COLUMN sync_interval INTEGER NOT NULL DEFAULT 0;
