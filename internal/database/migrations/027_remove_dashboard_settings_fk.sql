-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Remove FK constraint from dashboard_settings to support OIDC-only installs
-- SQLite doesn't support ALTER TABLE DROP CONSTRAINT, so recreate the table

PRAGMA foreign_keys=off;

CREATE TABLE dashboard_settings_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE,
    section_visibility TEXT NOT NULL DEFAULT '{}',
    section_order TEXT NOT NULL DEFAULT '[]',
    section_collapsed TEXT NOT NULL DEFAULT '{}',
    tracker_breakdown_sort_column TEXT DEFAULT 'uploaded',
    tracker_breakdown_sort_direction TEXT DEFAULT 'desc',
    tracker_breakdown_items_per_page INTEGER DEFAULT 15,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO dashboard_settings_new (id, user_id, section_visibility, section_order, section_collapsed, tracker_breakdown_sort_column, tracker_breakdown_sort_direction, tracker_breakdown_items_per_page, created_at, updated_at)
SELECT id, user_id, section_visibility, section_order, section_collapsed, tracker_breakdown_sort_column, tracker_breakdown_sort_direction, tracker_breakdown_items_per_page, created_at, updated_at
FROM dashboard_settings;
DROP TABLE dashboard_settings;
ALTER TABLE dashboard_settings_new RENAME TO dashboard_settings;

CREATE TRIGGER IF NOT EXISTS trg_dashboard_settings_updated
AFTER UPDATE ON dashboard_settings
BEGIN
    UPDATE dashboard_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;

PRAGMA foreign_keys=on;
