-- Add option to prefix custom category to original torrent category
-- When prefix_original_category is TRUE and use_custom_category is TRUE,
-- the cross-seed category becomes: {custom_category}/{original_category}

ALTER TABLE cross_seed_settings ADD COLUMN prefix_original_category BOOLEAN NOT NULL DEFAULT 0;
