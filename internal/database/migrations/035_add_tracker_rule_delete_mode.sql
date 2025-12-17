-- Add delete_mode column to tracker_rules table
-- Values: 'none' (default), 'delete', 'deleteWithFiles'
ALTER TABLE tracker_rules ADD COLUMN delete_mode TEXT NOT NULL DEFAULT 'none';
