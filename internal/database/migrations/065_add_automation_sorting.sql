-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add sorting_config column for configurable sorting of automations.
-- Stores JSON: {"type": "simple", "field": "SIZE", "direction": "DESC"} or {"type": "score", "scoreRules": [...]}
-- NULL means default (oldest first).
ALTER TABLE automations ADD COLUMN sorting_config TEXT;
