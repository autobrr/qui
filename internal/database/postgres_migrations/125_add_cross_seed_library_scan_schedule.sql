-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- The saved Library settings keep every field of the Library card, and the
-- Library Scan schedule. last_scheduled_run_at is the start of the last
-- scheduled run; the next one starts one interval later.
ALTER TABLE cross_seed_search_settings ADD COLUMN exclude_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_search_settings ADD COLUMN exclude_tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_search_settings ADD COLUMN max_added_age_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_search_settings ADD COLUMN skip_individual_episodes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_search_settings ADD COLUMN torznab_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE cross_seed_search_settings ADD COLUMN schedule_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_search_settings ADD COLUMN schedule_interval_hours INTEGER NOT NULL DEFAULT 24;
ALTER TABLE cross_seed_search_settings ADD COLUMN schedule_max_searches INTEGER NOT NULL DEFAULT 400;
ALTER TABLE cross_seed_search_settings ADD COLUMN schedule_max_age_days INTEGER NOT NULL DEFAULT 14;
ALTER TABLE cross_seed_search_settings ADD COLUMN last_scheduled_run_at TIMESTAMP;

-- A Library Scan run is scheduled or manual. A scheduled run that stops at its
-- cap keeps the cap and the number of torrents that still wait.
ALTER TABLE cross_seed_search_runs ADD COLUMN kind TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE cross_seed_search_runs ADD COLUMN search_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_search_runs ADD COLUMN waiting_torrents INTEGER NOT NULL DEFAULT 0;
