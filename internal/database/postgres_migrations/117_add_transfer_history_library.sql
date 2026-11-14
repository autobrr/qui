-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The library of each instance on each UTC day, from the last reading of the
-- day. unique_size is NULL for a day without a completed Copy groups build.
CREATE TABLE IF NOT EXISTS transfer_history_library_days (
    instance_id INTEGER NOT NULL,
    day         INTEGER NOT NULL,
    torrents    INTEGER NOT NULL,
    total_size  BIGINT NOT NULL,
    unique_size BIGINT,
    PRIMARY KEY (instance_id, day)
);

-- The state of each instance in each UTC hour. free_disk is the free disk
-- space that qBittorrent reported at the last reading of the hour. The speeds
-- are the highest that the readings of the hour hold, in bytes per second. A
-- reading holds the highest speed since the reading before it, so up to one
-- minute before the hour can count toward it.
CREATE TABLE IF NOT EXISTS transfer_history_instance_hours (
    instance_id        INTEGER NOT NULL,
    hour               INTEGER NOT NULL,
    free_disk          BIGINT NOT NULL,
    max_upload_speed   BIGINT NOT NULL,
    max_download_speed BIGINT NOT NULL,
    PRIMARY KEY (instance_id, hour)
);

-- The torrent count of each qBittorrent state at the last reading of each
-- UTC hour. A state with no torrent has no row.
CREATE TABLE IF NOT EXISTS transfer_history_state_hours (
    instance_id INTEGER NOT NULL,
    hour        INTEGER NOT NULL,
    state       TEXT NOT NULL,
    torrents    INTEGER NOT NULL,
    PRIMARY KEY (instance_id, hour, state)
);
