-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The filesystem mode a run's preview was built under. Deletion refuses when it
-- no longer matches the instance, so a preview walked over SFTP is never
-- confirmed as a local delete. Rows from before this column were all local.
ALTER TABLE orphan_scan_runs ADD COLUMN filesystem_mode TEXT NOT NULL DEFAULT 'local';
