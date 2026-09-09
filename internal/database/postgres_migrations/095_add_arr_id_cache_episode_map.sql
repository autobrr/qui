-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE arr_id_cache
    ADD COLUMN IF NOT EXISTS episode_map_season INTEGER,
    ADD COLUMN IF NOT EXISTS episode_map_episode INTEGER,
    ADD COLUMN IF NOT EXISTS episode_map_absolute INTEGER,
    ADD COLUMN IF NOT EXISTS episode_map_known INTEGER NOT NULL DEFAULT 0;
