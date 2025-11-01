-- Migration 011: Add external_programs table
-- This table stores configurations for external programs that can be executed from the torrent context menu

CREATE TABLE IF NOT EXISTS external_programs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    path TEXT NOT NULL,
    args_template TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    use_terminal INTEGER NOT NULL DEFAULT 1,
    path_mappings TEXT NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create index on enabled status for faster filtering
CREATE INDEX IF NOT EXISTS idx_external_programs_enabled ON external_programs(enabled);

-- Add path_mappings column for existing tables that don't have it yet
-- This ALTER will fail silently if the column already exists (which is expected for new installs)
ALTER TABLE external_programs ADD COLUMN path_mappings TEXT NOT NULL DEFAULT '[]';
