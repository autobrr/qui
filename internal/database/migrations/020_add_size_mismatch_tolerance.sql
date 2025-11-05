-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add size_mismatch_tolerance_percent column to cross_seed_settings table
-- This controls the acceptable size difference percentage when filtering search results
ALTER TABLE cross_seed_settings ADD COLUMN size_mismatch_tolerance_percent REAL NOT NULL DEFAULT 5.0;