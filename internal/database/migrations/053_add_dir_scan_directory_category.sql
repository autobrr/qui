-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Allow specifying a qBittorrent category per scan directory (overrides global default).
ALTER TABLE dir_scan_directories ADD COLUMN category TEXT;

