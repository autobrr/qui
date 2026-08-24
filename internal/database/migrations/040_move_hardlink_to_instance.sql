-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Move hardlink settings from global cross_seed_settings to per-instance configuration.
-- This allows different instances to have different hardlink settings (some local, some remote).
-- Hardlinks are disabled by default for all instances during migration.

ALTER TABLE instances ADD COLUMN use_hardlinks BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE instances ADD COLUMN hardlink_base_dir TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN hardlink_dir_preset TEXT NOT NULL DEFAULT 'flat';

-- Update the instances_view to include hardlink columns
DROP VIEW IF EXISTS instances_view;
CREATE VIEW instances_view AS
SELECT
    i.id,
    n.value AS name,
    h.value AS host,
    u.value AS username,
    i.password_encrypted,
    bu.value AS basic_username,
    i.basic_password_encrypted,
    i.tls_skip_verify,
    i.sort_order,
    i.is_active,
    i.has_local_filesystem_access,
    i.use_hardlinks,
    i.hardlink_base_dir,
    i.hardlink_dir_preset
FROM instances i
LEFT JOIN string_pool n ON i.name_id = n.id
LEFT JOIN string_pool h ON i.host_id = h.id
LEFT JOIN string_pool u ON i.username_id = u.id
LEFT JOIN string_pool bu ON i.basic_username_id = bu.id;

-- Note: We intentionally do NOT remove the columns from cross_seed_settings here.
-- A separate migration (041) will clean those up after frontend/backend are updated.
-- This allows for a safer rollout.
