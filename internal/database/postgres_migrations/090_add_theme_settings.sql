-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS theme_settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    theme_id TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'auto',
    variation TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
