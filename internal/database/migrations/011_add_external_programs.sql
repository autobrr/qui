-- Migration 011: Add external_programs table
-- This table stores configurations for external programs that can be executed from the torrent context menu

CREATE TABLE IF NOT EXISTS external_programs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    path TEXT NOT NULL,
    args_template TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    use_terminal INTEGER NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create index on enabled status for faster filtering
CREATE INDEX IF NOT EXISTS idx_external_programs_enabled ON external_programs(enabled);
