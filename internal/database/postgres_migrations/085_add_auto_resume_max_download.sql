-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Byte budget for auto-resuming incomplete cross-seed additions, in MiB.
-- Replaces the max(1 - tolerance, 0.90) percentage floor for new additions.
-- 0 means only fully complete torrents auto-resume.
ALTER TABLE cross_seed_settings ADD COLUMN auto_resume_max_download_mb INTEGER NOT NULL DEFAULT 50;
