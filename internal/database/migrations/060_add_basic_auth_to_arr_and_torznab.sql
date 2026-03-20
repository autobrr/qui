-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add optional HTTP Basic Auth support for ARR instances and Torznab indexers.
-- Mirrors qBittorrent instance basic auth semantics: username is stored in string_pool,
-- password is encrypted, and never returned in API responses.

-- ARR instances
ALTER TABLE arr_instances ADD COLUMN basic_username_id INTEGER REFERENCES string_pool(id);
ALTER TABLE arr_instances ADD COLUMN basic_password_encrypted TEXT;

DROP VIEW IF EXISTS arr_instances_view;
CREATE VIEW arr_instances_view AS
SELECT
    ai.id,
    ai.type,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    sp_basic_user.value AS basic_username,
    ai.basic_password_encrypted,
    ai.api_key_encrypted,
    ai.enabled,
    ai.priority,
    ai.timeout_seconds,
    ai.last_test_at,
    ai.last_test_status,
    ai.last_test_error,
    ai.created_at,
    ai.updated_at
FROM arr_instances ai
INNER JOIN string_pool sp_name ON ai.name_id = sp_name.id
INNER JOIN string_pool sp_base_url ON ai.base_url_id = sp_base_url.id
LEFT JOIN string_pool sp_basic_user ON ai.basic_username_id = sp_basic_user.id;

-- Torznab indexers
ALTER TABLE torznab_indexers ADD COLUMN basic_username_id INTEGER REFERENCES string_pool(id);
ALTER TABLE torznab_indexers ADD COLUMN basic_password_encrypted TEXT;

-- Recreate the view to expose new columns (includes limit columns introduced later).
DROP VIEW IF EXISTS torznab_indexers_view;
CREATE VIEW torznab_indexers_view AS
SELECT
    ti.id,
    sp_name.value AS name,
    sp_base_url.value AS base_url,
    sp_indexer_id.value AS indexer_id,
    sp_basic_user.value AS basic_username,
    ti.basic_password_encrypted,
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
LEFT JOIN string_pool sp_indexer_id ON ti.indexer_id_string_id = sp_indexer_id.id
LEFT JOIN string_pool sp_basic_user ON ti.basic_username_id = sp_basic_user.id;
