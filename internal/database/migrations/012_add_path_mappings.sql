-- Migration 012: Add path_mappings column to external_programs table
-- This adds support for path mapping to convert remote paths to local mount points

ALTER TABLE external_programs ADD COLUMN path_mappings TEXT NOT NULL DEFAULT '[]';
