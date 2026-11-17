-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- size is the size of the torrent of the request at the start of a Transfer.
-- The Transfers page shows it in its own column.
ALTER TABLE torrent_transfers ADD COLUMN size INTEGER NOT NULL DEFAULT 0;
