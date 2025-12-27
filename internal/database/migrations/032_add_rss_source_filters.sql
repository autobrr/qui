-- Add RSS source filtering columns for cross-seed automation
-- These filter which LOCAL torrents are considered when matching RSS feed items
-- Empty arrays mean "all" (no filtering)

ALTER TABLE cross_seed_settings ADD COLUMN rss_source_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_settings ADD COLUMN rss_source_tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_settings ADD COLUMN rss_source_exclude_categories TEXT NOT NULL DEFAULT '[]';
ALTER TABLE cross_seed_settings ADD COLUMN rss_source_exclude_tags TEXT NOT NULL DEFAULT '[]';
