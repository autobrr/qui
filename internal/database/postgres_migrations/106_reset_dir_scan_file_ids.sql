-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- file_id now stores the tagged hardlink.FileID form (one kind byte plus 24
-- identity bytes). Rows written in the old platform widths cannot be read
-- back, so they are cleared and the next scan fills them again. A file that
-- moves before that scan shows up as a new row.
UPDATE dir_scan_files SET file_id = NULL;
