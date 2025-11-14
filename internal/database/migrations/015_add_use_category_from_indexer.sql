-- 015_add_use_category_from_indexer.sql
-- Add use_category_from_indexer column to cross_seed_settings table

-- Add the new column with default value false
ALTER TABLE cross_seed_settings 
ADD COLUMN use_category_from_indexer BOOLEAN NOT NULL DEFAULT 0;