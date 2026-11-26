-- Add per-mode skip auto-resume settings for cross-seed
-- When enabled, torrents remain paused after hash check instead of auto-resuming
-- Default to false to preserve existing behavior

ALTER TABLE cross_seed_settings ADD COLUMN skip_auto_resume_rss BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN skip_auto_resume_seeded_search BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN skip_auto_resume_completion BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN skip_auto_resume_webhook BOOLEAN NOT NULL DEFAULT 0;
