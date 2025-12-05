-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS dashboard_settings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL UNIQUE,
    section_visibility TEXT NOT NULL DEFAULT '{}',
    section_order TEXT NOT NULL DEFAULT '[]',
    tracker_breakdown_sort_column TEXT DEFAULT 'uploaded',
    tracker_breakdown_sort_direction TEXT DEFAULT 'desc',
    tracker_breakdown_items_per_page INTEGER DEFAULT 15,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES user(id) ON DELETE CASCADE
);

CREATE TRIGGER IF NOT EXISTS trg_dashboard_settings_updated
AFTER UPDATE ON dashboard_settings
BEGIN
    UPDATE dashboard_settings SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
