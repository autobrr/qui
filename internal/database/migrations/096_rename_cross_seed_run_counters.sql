-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Seeded search runs: torrents_added counted cross-seeds applied, not source torrents.
-- Keep that value as cross_seeds_added and give the candidate bucket its own column.
ALTER TABLE cross_seed_search_runs ADD COLUMN cross_seeds_added INTEGER NOT NULL DEFAULT 0;
UPDATE cross_seed_search_runs SET cross_seeds_added = torrents_added;
ALTER TABLE cross_seed_search_runs RENAME COLUMN torrents_added TO torrents_with_cross_seeds;

-- RSS runs: the unit is a feed candidate, so name the counters after it.
ALTER TABLE cross_seed_runs RENAME COLUMN torrents_added TO cross_seeds_added;
ALTER TABLE cross_seed_runs RENAME COLUMN torrents_failed TO candidates_failed;
ALTER TABLE cross_seed_runs RENAME COLUMN torrents_skipped TO candidates_skipped;
