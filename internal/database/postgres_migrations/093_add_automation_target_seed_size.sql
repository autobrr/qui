-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add target_seed_size column for configurable target seed size in automations.
-- Stores JSON: {"enabled": true, "targetBytes": 1099511627776, "mode": "minimal", "scope": "tracker"}
-- NULL means no target seed size configured.
ALTER TABLE automations ADD COLUMN target_seed_size TEXT;

