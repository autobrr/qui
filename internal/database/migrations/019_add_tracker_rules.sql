-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS tracker_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    tracker_pattern TEXT NOT NULL,
    category TEXT,
    tag TEXT,
    upload_limit_kib INTEGER,
    download_limit_kib INTEGER,
    ratio_limit REAL,
    seeding_time_limit_minutes INTEGER,
    enabled INTEGER NOT NULL DEFAULT 1,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tracker_rules_instance ON tracker_rules(instance_id, sort_order, id);

CREATE TRIGGER IF NOT EXISTS trg_tracker_rules_updated
AFTER UPDATE ON tracker_rules
BEGIN
    UPDATE tracker_rules
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
