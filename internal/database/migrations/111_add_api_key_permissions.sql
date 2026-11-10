-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- API key Permissions and Instance limit, as JSON lists. An empty Instance
-- limit means every instance. Keys that exist before this migration get every
-- Permission (models.AllPermissions) and no Instance limit, so no integration
-- breaks on upgrade.
ALTER TABLE api_keys ADD COLUMN permissions TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_keys ADD COLUMN instance_ids TEXT NOT NULL DEFAULT '[]';

UPDATE api_keys SET permissions = '[{"area":"instances","level":"write"},{"area":"torrents","level":"write"},{"area":"crossSeed","level":"write"},{"area":"dirScan","level":"write"},{"area":"automations","level":"write"},{"area":"rss","level":"write"},{"area":"backups","level":"write"},{"area":"orphanScan","level":"write"},{"area":"externalPrograms","level":"read"},{"area":"externalPrograms","level":"run"},{"area":"indexers","level":"write"},{"area":"arr","level":"write"},{"area":"notifications","level":"write"},{"area":"settings","level":"write"},{"area":"logs","level":"write"},{"area":"system","level":"read"}]';

DROP VIEW IF EXISTS api_keys_view;
CREATE VIEW api_keys_view AS
SELECT
    ak.id,
    ak.key_hash,
    sp.value AS name,
    ak.created_at,
    ak.last_used_at,
    ak.permissions,
    ak.instance_ids
FROM api_keys ak
INNER JOIN string_pool sp ON ak.name_id = sp.id;
