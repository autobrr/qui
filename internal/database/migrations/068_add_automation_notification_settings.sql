-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add per-rule notification toggle for automation outcomes.
-- Defaults to 1 (enabled) to preserve existing behavior where
-- notifications are always sent when an automation has activity.
ALTER TABLE automations ADD COLUMN notify INTEGER NOT NULL DEFAULT 1;
