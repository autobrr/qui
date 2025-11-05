-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add find_individual_episodes column to cross_seed_settings table
-- This controls whether season packs can match with individual episodes during cross-seeding
ALTER TABLE cross_seed_settings ADD COLUMN find_individual_episodes BOOLEAN NOT NULL DEFAULT 0;