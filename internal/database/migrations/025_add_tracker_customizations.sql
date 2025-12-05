-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Tracker customizations: nicknames and merged tracker domains
CREATE TABLE IF NOT EXISTS tracker_customizations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    display_name TEXT NOT NULL,
    domains TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tracker_customizations_domains ON tracker_customizations(domains);

CREATE TRIGGER IF NOT EXISTS trg_tracker_customizations_updated
AFTER UPDATE ON tracker_customizations
BEGIN
    UPDATE tracker_customizations SET updated_at = CURRENT_TIMESTAMP WHERE id = NEW.id;
END;
