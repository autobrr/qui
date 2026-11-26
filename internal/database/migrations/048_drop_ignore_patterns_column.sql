-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Remove ignore_patterns column from cross_seed_settings.
-- File patterns to ignore during matching are now hardcoded.

ALTER TABLE cross_seed_settings DROP COLUMN ignore_patterns;
