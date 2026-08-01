-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Allow qui to assemble season packs from seeded episodes on its own initiative
-- (RSS/automation diversion). Separate from season_pack_enabled, which only
-- authorizes the autobrr webhook flow.
ALTER TABLE cross_seed_settings ADD COLUMN season_pack_automation_enabled INTEGER NOT NULL DEFAULT 0;
