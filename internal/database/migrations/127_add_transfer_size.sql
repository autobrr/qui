-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- size is the size of the torrent of the request at the start of a Transfer.
-- The Transfers page shows it in its own column.
ALTER TABLE torrent_transfers ADD COLUMN size INTEGER NOT NULL DEFAULT 0;

-- The Transfers page and the sidebar count the Transfers by state, and the
-- page lists the Transfers of one state.
CREATE INDEX IF NOT EXISTS torrent_transfers_state ON torrent_transfers (state);

-- qui looks up the Transfers of one torrent by its hash: before a rule starts
-- a Transfer, and in the cleanup of old rows, for each permanent failure.
CREATE INDEX IF NOT EXISTS torrent_transfer_members_hash ON torrent_transfer_members (hash, transfer_id);
