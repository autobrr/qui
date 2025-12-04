-- Add setting to control .cross category suffix behavior
-- Default TRUE preserves existing behavior where cross-seeds get .cross suffix
-- When FALSE, cross-seeds use the same category as the matched torrent

ALTER TABLE cross_seed_settings ADD COLUMN use_cross_category_suffix BOOLEAN NOT NULL DEFAULT TRUE;
