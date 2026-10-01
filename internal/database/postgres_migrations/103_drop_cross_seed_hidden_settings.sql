-- The UI has no control for these settings. Cross-seeds start paused by
-- default and get no default category.

ALTER TABLE cross_seed_settings
    DROP COLUMN start_paused,
    DROP COLUMN category;
