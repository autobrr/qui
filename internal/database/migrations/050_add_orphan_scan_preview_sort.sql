-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add preview sorting preference for orphan scan preview lists.
ALTER TABLE orphan_scan_settings ADD COLUMN preview_sort TEXT NOT NULL DEFAULT 'size_desc';
