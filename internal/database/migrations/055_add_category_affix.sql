-- Add category affix columns to replace the old use_cross_category_suffix boolean
-- use_cross_category_affix: enable/disable the affix feature
-- category_affix_mode: 'prefix' or 'suffix'
-- category_affix: the actual affix value (e.g., '.cross', 'cross-seed/')

ALTER TABLE cross_seed_settings ADD COLUMN use_cross_category_affix INTEGER NOT NULL DEFAULT 1;
ALTER TABLE cross_seed_settings ADD COLUMN category_affix_mode TEXT NOT NULL DEFAULT 'suffix';
ALTER TABLE cross_seed_settings ADD COLUMN category_affix TEXT NOT NULL DEFAULT '.cross';

UPDATE cross_seed_settings SET use_cross_category_affix = use_cross_category_suffix;

-- Note: use_cross_category_suffix is deprecated but retained for rollback safety.
-- Should be removed in the future after this release is stable.
