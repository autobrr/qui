-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add optional per-rule interval for automation trigger frequency.
-- NULL means use global default (20s). Custom values enforced >= 60s at API level.

ALTER TABLE automations ADD COLUMN interval_seconds INTEGER;
