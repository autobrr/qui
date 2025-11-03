-- Add backend column to torznab_indexers to differentiate between Jackett, Prowlarr and native Torznab backends
ALTER TABLE torznab_indexers
    ADD COLUMN backend TEXT NOT NULL DEFAULT 'jackett'
    CHECK(backend IN ('jackett', 'prowlarr', 'native'));

-- Refresh torznab_indexers_view to expose the backend column
DROP VIEW IF EXISTS torznab_indexers_view;
CREATE VIEW torznab_indexers_view AS
SELECT
    ti.id,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    sp_indexer_id.value AS indexer_id,
    ti.backend,
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
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id
LEFT JOIN string_pool sp_indexer_id ON ti.indexer_id_string_id = sp_indexer_id.id;
