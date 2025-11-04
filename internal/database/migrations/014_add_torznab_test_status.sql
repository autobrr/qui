-- Add test status tracking to torznab_indexers
-- Tracks the last test result and error message
ALTER TABLE torznab_indexers ADD COLUMN last_test_at TIMESTAMP NULL;
ALTER TABLE torznab_indexers ADD COLUMN last_test_status TEXT DEFAULT 'unknown' CHECK(last_test_status IN ('unknown', 'ok', 'error'));
ALTER TABLE torznab_indexers ADD COLUMN last_test_error TEXT NULL;

-- Update the view to include test status fields
DROP VIEW IF EXISTS torznab_indexers_view;
CREATE VIEW torznab_indexers_view AS
SELECT 
    ti.id,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    ti.api_key_encrypted,
    ti.enabled,
    ti.priority,
    ti.timeout_seconds,
    ti.last_test_at,
    ti.last_test_status,
    ti.last_test_error,
    ti.created_at,
    ti.updated_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id;
