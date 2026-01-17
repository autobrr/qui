-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add limit columns to torznab_indexers (sensible default is 100)
ALTER TABLE torznab_indexers ADD COLUMN limit_default INTEGER NOT NULL DEFAULT 100;
ALTER TABLE torznab_indexers ADD COLUMN limit_max INTEGER NOT NULL DEFAULT 100;

UPDATE torznab_indexers SET limit_default = 100 WHERE limit_default <= 0;
UPDATE torznab_indexers SET limit_max = 100 WHERE limit_max <= 0;

-- Recreate the view to expose new columns
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
    ti.limit_default,
    ti.limit_max,
    ti.last_test_at,
    ti.last_test_status,
    ti.last_test_error,
    ti.created_at,
    ti.updated_at
FROM torznab_indexers ti
INNER JOIN string_pool sp_name ON ti.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ti.base_url_id = sp_base_url.id
LEFT JOIN string_pool sp_indexer_id ON ti.indexer_id_string_id = sp_indexer_id.id;
