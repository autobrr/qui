-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

UPDATE cross_seed_search_settings
SET gazelle_sites = '["orpheus.network","redacted.sh"]'
WHERE torznab_enabled = 0
  AND COALESCE(TRIM(gazelle_sites), '') IN ('', '[]', 'null');

UPDATE cross_seed_search_settings
SET indexer_ids = '[]'
WHERE torznab_enabled = 0;

ALTER TABLE cross_seed_search_settings DROP COLUMN torznab_enabled;
