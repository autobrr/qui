-- Add custom category option for cross-seeds
-- When use_custom_category is TRUE, cross-seeds use the exact custom_category value
-- without any automatic suffixing

ALTER TABLE cross_seed_settings ADD COLUMN use_custom_category BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN custom_category TEXT DEFAULT '';
