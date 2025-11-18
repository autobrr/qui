-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS instance_reannounce_settings (
    instance_id INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 0,
    initial_wait_seconds INTEGER NOT NULL DEFAULT 15,
    reannounce_interval_seconds INTEGER NOT NULL DEFAULT 7,
    max_age_seconds INTEGER NOT NULL DEFAULT 600,
    monitor_all INTEGER NOT NULL DEFAULT 1,
    categories_json TEXT NOT NULL DEFAULT '[]',
    tags_json TEXT NOT NULL DEFAULT '[]',
    trackers_json TEXT NOT NULL DEFAULT '[]',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS trg_instance_reannounce_settings_updated
AFTER UPDATE ON instance_reannounce_settings
BEGIN
    UPDATE instance_reannounce_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE instance_id = NEW.instance_id;
END;
