-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The fold of the Detail retention moves the days of each torrent before a UTC
-- day into one row for each year. torrent_days_before is the latest such day,
-- counted since 1970-01-01, so that a query adds the yearly rows only to a
-- period that starts before it. No row exists until the first fold.
CREATE TABLE IF NOT EXISTS transfer_history_folds (
    id                  INTEGER PRIMARY KEY CHECK (id = 1),
    torrent_days_before INTEGER NOT NULL
);
