-- Add optional hourly/daily request limit metadata to Torznab indexers and refresh the view
ALTER TABLE torznab_indexers
    ADD COLUMN hourly_request_limit INTEGER;

ALTER TABLE torznab_indexers
    ADD COLUMN daily_request_limit INTEGER;

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
    ti.hourly_request_limit,
    ti.daily_request_limit,
    ti.last_test_at,
    ti.last_test_status,
    ti.last_test_error,
    ti.created_at,
    ti.updated_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id
LEFT JOIN string_pool sp_indexer_id ON ti.indexer_id_string_id = sp_indexer_id.id;
