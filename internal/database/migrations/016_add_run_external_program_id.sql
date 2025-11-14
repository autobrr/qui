-- 016_add_run_external_program_id.sql
-- Add run_external_program_id column to cross_seed_settings table

-- Add the new column with default value null
ALTER TABLE cross_seed_settings 
ADD COLUMN run_external_program_id INTEGER NULL;

-- Add foreign key constraint to external_programs table
-- Note: SQLite doesn't support adding foreign key constraints to existing tables,
-- but we can add an index to improve performance when querying by program ID
CREATE INDEX IF NOT EXISTS idx_cross_seed_settings_external_program ON cross_seed_settings(run_external_program_id);