-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE dir_scan_settings
ADD COLUMN max_searchee_age_days INTEGER NOT NULL DEFAULT 0;
