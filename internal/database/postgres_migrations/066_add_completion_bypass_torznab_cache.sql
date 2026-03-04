-- Add completion bypass_torznab_cache flag for postgres parity with sqlite migration 065.

ALTER TABLE instance_crossseed_completion_settings
    ADD COLUMN bypass_torznab_cache INTEGER NOT NULL DEFAULT 0;
