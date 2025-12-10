-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS instance_crossseed_completion_settings (
    instance_id INTEGER PRIMARY KEY REFERENCES instances(id) ON DELETE CASCADE,
    enabled INTEGER NOT NULL DEFAULT 0,
    categories_json TEXT NOT NULL DEFAULT '[]',
    tags_json TEXT NOT NULL DEFAULT '[]',
    exclude_categories_json TEXT NOT NULL DEFAULT '[]',
    exclude_tags_json TEXT NOT NULL DEFAULT '[]',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS trg_instance_crossseed_completion_settings_updated
AFTER UPDATE ON instance_crossseed_completion_settings
BEGIN
    UPDATE instance_crossseed_completion_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE instance_id = NEW.instance_id;
END;
