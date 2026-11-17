-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The print of the torrent's maindata entry when qui cached its file list, and
-- the last_synced_at of the write that stored it. A write by an image that does
-- not know the print moves last_synced_at only, so the print then reads as unset.
ALTER TABLE torrent_files_sync ADD COLUMN maindata_print BIGINT;
ALTER TABLE torrent_files_sync ADD COLUMN print_synced_at TIMESTAMP;
