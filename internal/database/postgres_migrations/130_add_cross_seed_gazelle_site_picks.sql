-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE instance_crossseed_completion_settings
    ADD COLUMN IF NOT EXISTS gazelle_sites_json TEXT NOT NULL DEFAULT '[]';

UPDATE instance_crossseed_completion_settings
SET gazelle_sites_json = '["orpheus.network","redacted.sh"]'
WHERE COALESCE(TRIM(indexer_ids_json), '') NOT IN ('', '[]', 'null');

ALTER TABLE cross_seed_search_settings
    ADD COLUMN IF NOT EXISTS gazelle_sites TEXT NOT NULL DEFAULT '[]';

UPDATE cross_seed_search_settings
SET gazelle_sites = '["orpheus.network","redacted.sh"]'
WHERE COALESCE(TRIM(indexer_ids), '') NOT IN ('', '[]', 'null');
