-- Add source-specific tagging columns for cross-seed automation
-- Each source type (RSS, Seeded Search, Completion, Webhook) can have its own tags
-- Default is ["cross-seed"] for all sources

ALTER TABLE cross_seed_settings
    ADD COLUMN rss_automation_tags TEXT NOT NULL DEFAULT '["cross-seed"]';

ALTER TABLE cross_seed_settings
    ADD COLUMN seeded_search_tags TEXT NOT NULL DEFAULT '["cross-seed"]';

ALTER TABLE cross_seed_settings
    ADD COLUMN completion_search_tags TEXT NOT NULL DEFAULT '["cross-seed"]';

ALTER TABLE cross_seed_settings
    ADD COLUMN webhook_tags TEXT NOT NULL DEFAULT '["cross-seed"]';

ALTER TABLE cross_seed_settings
    ADD COLUMN inherit_source_tags BOOLEAN NOT NULL DEFAULT 0;
