-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add dry run toggle for automations (simulate actions without applying changes).
ALTER TABLE automations ADD COLUMN dry_run INTEGER NOT NULL DEFAULT 0;
