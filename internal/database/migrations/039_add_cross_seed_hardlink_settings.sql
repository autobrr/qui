-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add hardlink mode settings for cross-seeding.
-- When enabled with local filesystem access, creates hardlinked file trees
-- instead of using reuse+rename alignment.

ALTER TABLE cross_seed_settings ADD COLUMN use_hardlinks BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN hardlink_base_dir TEXT NOT NULL DEFAULT '';
ALTER TABLE cross_seed_settings ADD COLUMN hardlink_dir_preset TEXT NOT NULL DEFAULT 'flat';
