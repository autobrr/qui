-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Remove legacy global completion columns from cross_seed_settings.
-- These were replaced by per-instance settings in instance_crossseed_completion_settings (migration 029).

-- SQLite does not support DROP COLUMN directly, so we must recreate the table.
-- Step 1: Create new table without the completion columns
CREATE TABLE cross_seed_settings_new (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT 0,
    run_interval_minutes INTEGER NOT NULL DEFAULT 120,
    start_paused BOOLEAN NOT NULL DEFAULT 1,
    category TEXT,
    ignore_patterns TEXT NOT NULL DEFAULT '[]',
    target_instance_ids TEXT NOT NULL DEFAULT '[]',
    target_indexer_ids TEXT NOT NULL DEFAULT '[]',
    max_results_per_run INTEGER NOT NULL DEFAULT 50,
    find_individual_episodes BOOLEAN NOT NULL DEFAULT 0,
    size_mismatch_tolerance_percent REAL NOT NULL DEFAULT 5.0,
    use_category_from_indexer BOOLEAN NOT NULL DEFAULT 0,
    run_external_program_id INTEGER REFERENCES external_programs(id) ON DELETE SET NULL,
    rss_automation_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
    seeded_search_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
    completion_search_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
    webhook_tags TEXT NOT NULL DEFAULT '["cross-seed"]',
    inherit_source_tags BOOLEAN NOT NULL DEFAULT 0,
    use_cross_category_suffix BOOLEAN NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Step 2: Copy data from old table (excluding dropped columns)
INSERT INTO cross_seed_settings_new (
    id, enabled, run_interval_minutes, start_paused, category,
    ignore_patterns, target_instance_ids, target_indexer_ids,
    max_results_per_run, find_individual_episodes, size_mismatch_tolerance_percent,
    use_category_from_indexer, run_external_program_id,
    rss_automation_tags, seeded_search_tags, completion_search_tags,
    webhook_tags, inherit_source_tags, use_cross_category_suffix,
    created_at, updated_at
)
SELECT
    id, enabled, run_interval_minutes, start_paused, category,
    ignore_patterns, target_instance_ids, target_indexer_ids,
    max_results_per_run, find_individual_episodes, size_mismatch_tolerance_percent,
    use_category_from_indexer, run_external_program_id,
    rss_automation_tags, seeded_search_tags, completion_search_tags,
    webhook_tags, inherit_source_tags, use_cross_category_suffix,
    created_at, updated_at
FROM cross_seed_settings;

-- Step 3: Drop old table
DROP TABLE cross_seed_settings;

-- Step 4: Rename new table
ALTER TABLE cross_seed_settings_new RENAME TO cross_seed_settings;

-- Step 5: Recreate the update trigger
CREATE TRIGGER IF NOT EXISTS trg_cross_seed_settings_updated
AFTER UPDATE ON cross_seed_settings
BEGIN
    UPDATE cross_seed_settings
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
