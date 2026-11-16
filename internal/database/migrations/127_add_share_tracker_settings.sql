-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The Share tracker settings, in one row. Without the row, port is 0 and the
-- tracker never listens. public_address is the URL at which recipients reach
-- the tracker. trusted_proxies is a JSON array of the addresses and CIDR
-- prefixes whose X-Forwarded-For the tracker trusts.
CREATE TABLE IF NOT EXISTS share_tracker_settings (
    id              INTEGER PRIMARY KEY CHECK (id = 1),
    port            INTEGER NOT NULL,
    public_address  TEXT NOT NULL,
    trusted_proxies TEXT NOT NULL
);
