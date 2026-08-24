-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add auto-cleanup settings to orphan scan
ALTER TABLE orphan_scan_settings ADD COLUMN auto_cleanup_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orphan_scan_settings ADD COLUMN auto_cleanup_max_files INTEGER NOT NULL DEFAULT 100;
