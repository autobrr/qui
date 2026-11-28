-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE cross_seed_blocklist ADD COLUMN torrent_name TEXT NOT NULL DEFAULT '';
ALTER TABLE cross_seed_blocklist ADD COLUMN tracker TEXT NOT NULL DEFAULT '';
