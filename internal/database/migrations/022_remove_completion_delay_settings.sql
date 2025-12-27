-- Remove completion delay settings (no longer needed with .cross category suffixing)
-- Cross-seeded torrents now go into .cross suffixed categories (e.g., movies.cross)
-- which prevents *arr applications from importing them, making delay unnecessary.

-- Note: SQLite 3.35.0+ supports DROP COLUMN
ALTER TABLE cross_seed_settings
    DROP COLUMN completion_delay_minutes;

ALTER TABLE cross_seed_settings
    DROP COLUMN completion_pre_import_categories;
