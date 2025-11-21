-- Add completion-triggered cross-seed automation fields
ALTER TABLE cross_seed_settings
    ADD COLUMN completion_enabled BOOLEAN NOT NULL DEFAULT 0;

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_categories TEXT NOT NULL DEFAULT '[]';

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_tags TEXT NOT NULL DEFAULT '[]';

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_exclude_categories TEXT NOT NULL DEFAULT '[]';

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_exclude_tags TEXT NOT NULL DEFAULT '[]';
