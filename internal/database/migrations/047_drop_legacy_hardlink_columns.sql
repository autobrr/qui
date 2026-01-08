-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Remove legacy hardlink columns from cross_seed_settings.
-- These were moved to the instances table in migration 040.
-- Migration 040 noted that cleanup would happen in a later migration - this is it.

ALTER TABLE cross_seed_settings DROP COLUMN use_hardlinks;
ALTER TABLE cross_seed_settings DROP COLUMN hardlink_base_dir;
ALTER TABLE cross_seed_settings DROP COLUMN hardlink_dir_preset;
