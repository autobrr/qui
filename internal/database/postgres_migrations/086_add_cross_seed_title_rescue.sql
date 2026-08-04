-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE cross_seed_settings ADD COLUMN rescue_title_mismatches INTEGER NOT NULL DEFAULT 0;
