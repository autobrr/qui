-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Key theme_settings by slot, so the mobile layout can keep its own theme.
-- The mobile row is the opt-in (ADR 0015). SQLite cannot drop a column CHECK,
-- so the table is rebuilt; Postgres does the same to end with the same table.
CREATE TABLE theme_settings_new (
    slot TEXT PRIMARY KEY CHECK (slot IN ('default', 'mobile')),
    theme_id TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'auto',
    variation TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO theme_settings_new (slot, theme_id, mode, variation, updated_at)
SELECT 'default', theme_id, mode, variation, updated_at FROM theme_settings WHERE id = 1;

DROP TABLE theme_settings;

ALTER TABLE theme_settings_new RENAME TO theme_settings;
