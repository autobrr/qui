-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- What a run's walk had. A remote run deletes only when the walk read every
-- file's identity, since without it a second path to a torrent's file reads as
-- an orphan, and only on the SSH user, host, port, pinned host key and
-- private key it walked. Rows from before these columns read as incomplete,
-- so an old remote preview stays refused.
ALTER TABLE orphan_scan_runs ADD COLUMN identity_complete INTEGER NOT NULL DEFAULT 0;
ALTER TABLE orphan_scan_runs ADD COLUMN filesystem_endpoint TEXT NOT NULL DEFAULT '';
