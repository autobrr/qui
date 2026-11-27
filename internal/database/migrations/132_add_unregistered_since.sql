-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS unregistered_since (
    instance_id  INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    torrent_hash TEXT NOT NULL,
    since        DATETIME NOT NULL,
    PRIMARY KEY (instance_id, torrent_hash)
);
