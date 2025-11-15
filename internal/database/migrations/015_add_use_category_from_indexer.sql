-- 015_add_use_category_from_indexer.sql
-- Add new cross-seed settings columns in a single migration so existing deployments only run one ALTER TABLE

-- Add use_category_from_indexer column with default value false
ALTER TABLE cross_seed_settings
ADD COLUMN use_category_from_indexer BOOLEAN NOT NULL DEFAULT 0;

-- Add run_external_program_id column with default NULL
ALTER TABLE cross_seed_settings
ADD COLUMN run_external_program_id INTEGER NULL;

-- Add index to help lookups by the optional external program
CREATE INDEX IF NOT EXISTS idx_cross_seed_settings_external_program ON cross_seed_settings(run_external_program_id);
