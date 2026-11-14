-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The RED/OPS summary of a Library Scan run, as JSON: for each site, the number
-- of torrents asked, matched, failed, and not asked for each reason. Runs from
-- before this column have none.
ALTER TABLE cross_seed_search_runs ADD COLUMN gazelle_summary_json TEXT;
