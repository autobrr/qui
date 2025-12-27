-- Add completion delay settings for *arr import timing
-- delay_minutes: time to wait after completion before triggering cross-seed search
-- pre_import_categories: categories that indicate torrent is waiting for *arr import
-- If category changes from a pre-import category, search triggers immediately (skipping delay)

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_delay_minutes INTEGER NOT NULL DEFAULT 0;

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_pre_import_categories TEXT NOT NULL DEFAULT '[]';
