-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add included_in_stats column to tracker_customizations
-- Stores comma-separated list of secondary domains whose stats SHOULD be included in combined totals
-- Empty = only primary domain stats shown (backwards compatible with pre-feature behavior)
ALTER TABLE tracker_customizations ADD COLUMN included_in_stats TEXT DEFAULT '';
