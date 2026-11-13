-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The Statistics settings, in one row. Without the row, recording is on and
-- detail_retention_days is 400. detail_retention_days is the Detail
-- retention: 90 or 400, or 0 to keep the detail forever.
CREATE TABLE IF NOT EXISTS transfer_history_settings (
    id                    INTEGER PRIMARY KEY CHECK (id = 1),
    recording             BOOLEAN NOT NULL,
    detail_retention_days INTEGER NOT NULL
);

-- The daily bytes of a torrent after the Detail retention, folded into one
-- row for each UTC year.
CREATE TABLE IF NOT EXISTS transfer_history_torrent_years (
    torrent_id INTEGER NOT NULL REFERENCES transfer_history_torrents(id) ON DELETE CASCADE,
    year       INTEGER NOT NULL,
    uploaded   INTEGER NOT NULL,
    downloaded INTEGER NOT NULL,
    PRIMARY KEY (torrent_id, year)
) WITHOUT ROWID;
