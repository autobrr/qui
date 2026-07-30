-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Saved torrent filter views (discussion #964).
-- filters holds the opaque TorrentFilters JSON blob; the backend never
-- interprets its shape, so it stays plain TEXT (no string_pool interning).
-- No FK to user(id): OIDC-only and auth-disabled installs never populate the
-- user table (see 027_remove_dashboard_settings_fk.sql).
-- No updated_at trigger either; the store sets the column so both engines agree.
CREATE TABLE IF NOT EXISTS filter_views (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    filters TEXT NOT NULL DEFAULT '{}',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (user_id, name)
);
