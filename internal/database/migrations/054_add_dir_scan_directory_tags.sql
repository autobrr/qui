-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Allow specifying additional qBittorrent tags per scan directory.
ALTER TABLE dir_scan_directories ADD COLUMN tags TEXT;

