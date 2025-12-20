-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Drop existing tracker rules tables completely
DROP TRIGGER IF EXISTS trg_tracker_rules_updated;
DROP INDEX IF EXISTS idx_tracker_rule_activity_instance_created;
DROP INDEX IF EXISTS idx_tracker_rules_instance;
DROP TABLE IF EXISTS tracker_rule_activity;
DROP TABLE IF EXISTS tracker_rules;

-- Create new automations table
CREATE TABLE IF NOT EXISTS automations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    tracker_pattern TEXT NOT NULL,
    category TEXT,
    tag TEXT,
    tag_match_mode TEXT DEFAULT 'any',
    upload_limit_kib INTEGER,
    download_limit_kib INTEGER,
    ratio_limit REAL,
    seeding_time_limit_minutes INTEGER,
    delete_mode TEXT NOT NULL DEFAULT 'none',
    delete_unregistered INTEGER NOT NULL DEFAULT 0,
    delete_unregistered_min_age INTEGER,
    conditions TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_automations_instance ON automations(instance_id, sort_order, id);

CREATE TRIGGER IF NOT EXISTS trg_automations_updated
AFTER UPDATE ON automations
BEGIN
    UPDATE automations
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

-- Create new automation_activity table
CREATE TABLE IF NOT EXISTS automation_activity (
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

CREATE INDEX IF NOT EXISTS idx_automation_activity_instance_created
    ON automation_activity(instance_id, created_at DESC);
