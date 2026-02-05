-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS notification_targets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    event_types TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notification_targets_enabled ON notification_targets(enabled);
