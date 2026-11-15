-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The daily bytes of each torrent on the days that a write still adds to. The
-- key starts with the day, so that the rows of one day sit on a few pages and a
-- write changes few pages. When a reading starts a new UTC day, qui moves the
-- rows of the days before it into transfer_history_torrent_days. instance_id
-- is the instance of the torrent, so that a read of one instance needs no join.
CREATE TABLE IF NOT EXISTS transfer_history_recent_torrent_days (
    day         INTEGER NOT NULL,
    instance_id INTEGER NOT NULL,
    torrent_id  INTEGER NOT NULL REFERENCES transfer_history_torrents(id) ON DELETE CASCADE,
    uploaded    INTEGER NOT NULL,
    downloaded  INTEGER NOT NULL,
    PRIMARY KEY (day, torrent_id)
) WITHOUT ROWID;

-- The delete of a torrent finds its rows through this index.
CREATE INDEX IF NOT EXISTS transfer_history_recent_torrent_days_torrent
    ON transfer_history_recent_torrent_days (torrent_id);
