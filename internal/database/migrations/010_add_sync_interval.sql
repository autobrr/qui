-- Add sync_interval column to instances table
-- sync_interval: Number of minutes between automatic sync updates (0 = disabled, minimum 1 minute)
-- Timer resets after any sync operation (periodic, manual, or proxy-initiated)
ALTER TABLE instances ADD COLUMN sync_interval INTEGER NOT NULL DEFAULT 0;
