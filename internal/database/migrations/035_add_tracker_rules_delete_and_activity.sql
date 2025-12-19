-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add delete mode and related columns to tracker_rules
ALTER TABLE tracker_rules ADD COLUMN delete_mode TEXT NOT NULL DEFAULT 'none';
ALTER TABLE tracker_rules ADD COLUMN delete_unregistered INTEGER NOT NULL DEFAULT 0;
ALTER TABLE tracker_rules ADD COLUMN delete_unregistered_min_age INTEGER;
ALTER TABLE tracker_rules ADD COLUMN tag_match_mode TEXT DEFAULT 'any';
ALTER TABLE tracker_rules ADD COLUMN conditions TEXT;

-- Create activity tracking table
CREATE TABLE IF NOT EXISTS tracker_rule_activity (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL,
    hash TEXT NOT NULL,
    torrent_name TEXT,
    tracker_domain TEXT,
    action TEXT NOT NULL,
    rule_id INTEGER,
    rule_name TEXT,
    outcome TEXT NOT NULL,
    reason TEXT,
    details TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tracker_rule_activity_instance_created
    ON tracker_rule_activity(instance_id, created_at DESC);
