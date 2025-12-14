-- Add webhook source filtering columns for cross-seed automation.
-- These filter which LOCAL torrents are considered when checking webhook requests.
-- Empty arrays mean "all" (no filtering).

ALTER TABLE cross_seed_settings ADD COLUMN webhook_source_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_settings ADD COLUMN webhook_source_tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_settings ADD COLUMN webhook_source_exclude_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_settings ADD COLUMN webhook_source_exclude_tags TEXT NOT NULL DEFAULT '[]';
