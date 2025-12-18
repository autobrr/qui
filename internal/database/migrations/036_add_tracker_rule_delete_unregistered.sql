-- Add delete_unregistered flag to tracker_rules
-- When enabled, torrents matching this rule that are no longer registered
-- with the tracker will be automatically deleted using the rule's delete_mode
ALTER TABLE tracker_rules ADD COLUMN delete_unregistered INTEGER NOT NULL DEFAULT 0;
