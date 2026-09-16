-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Opt-in scan roots beyond the ones derived from torrent save paths, plus the
-- abandoned-directory option. Without them the scan only walks directories some
-- torrent already points at, so files sitting directly in the default save path
-- and empty directories left behind by moves are never seen.
ALTER TABLE orphan_scan_settings ADD COLUMN scan_default_save_path INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orphan_scan_settings ADD COLUMN scan_category_paths INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orphan_scan_settings ADD COLUMN delete_abandoned_dirs INTEGER NOT NULL DEFAULT 0;

-- Abandoned directories share the run's preview table with orphan files.
ALTER TABLE orphan_scan_files ADD COLUMN is_abandoned_dir INTEGER NOT NULL DEFAULT 0;
