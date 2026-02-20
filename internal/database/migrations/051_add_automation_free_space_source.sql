-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add free_space_source column for configurable free space source in automations.
-- Stores JSON: {"type": "qbittorrent"} or {"type": "path", "path": "/mnt/data"}
-- NULL means default (qbittorrent free space).
ALTER TABLE automations ADD COLUMN free_space_source TEXT;
