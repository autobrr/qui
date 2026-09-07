-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE orphan_scan_settings ADD COLUMN scan_default_save_path INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orphan_scan_settings ADD COLUMN scan_category_paths INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orphan_scan_settings ADD COLUMN delete_abandoned_dirs INTEGER NOT NULL DEFAULT 0;

ALTER TABLE orphan_scan_files ADD COLUMN is_dir INTEGER NOT NULL DEFAULT 0;
