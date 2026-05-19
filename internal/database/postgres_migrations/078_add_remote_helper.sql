-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE instances ADD COLUMN ssh_host                     TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_port                     INTEGER NOT NULL DEFAULT 22;
ALTER TABLE instances ADD COLUMN ssh_username                 TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_auth_type                TEXT NOT NULL DEFAULT '' CHECK (ssh_auth_type IN ('', 'key', 'password'));
ALTER TABLE instances ADD COLUMN ssh_key_encrypted            TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_key_passphrase_encrypted TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_password_encrypted       TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_host_key                 TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN helper_path                  TEXT NOT NULL DEFAULT '~/.config/qui-helper/qui-helper';
ALTER TABLE instances ADD COLUMN helper_version               TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN helper_capabilities          TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN helper_allowed_roots         TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN helper_reflink_roots         TEXT NOT NULL DEFAULT '[]';
ALTER TABLE instances ADD COLUMN helper_platform              TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN helper_deployed_at           TIMESTAMP;
ALTER TABLE instances ADD COLUMN helper_last_activity_at      TIMESTAMP;

DROP VIEW IF EXISTS instances_view;
CREATE VIEW instances_view AS
SELECT
    i.id,
    n.value AS name,
    h.value AS host,
    u.value AS username,
    i.password_encrypted,
    i.api_key_encrypted,
    bu.value AS basic_username,
    i.basic_password_encrypted,
    i.tls_skip_verify,
    i.sort_order,
    i.is_active,
    i.has_local_filesystem_access,
    i.use_hardlinks,
    i.hardlink_base_dir,
    i.hardlink_dir_preset,
    i.use_reflinks,
    i.fallback_to_regular_mode,
    i.ssh_host,
    i.ssh_port,
    i.ssh_username,
    i.ssh_auth_type,
    i.ssh_key_encrypted,
    i.ssh_key_passphrase_encrypted,
    i.ssh_password_encrypted,
    i.ssh_host_key,
    i.helper_path,
    i.helper_version,
    i.helper_capabilities,
    i.helper_allowed_roots,
    i.helper_reflink_roots,
    i.helper_platform,
    i.helper_deployed_at,
    i.helper_last_activity_at
FROM instances i
LEFT JOIN string_pool n ON i.name_id = n.id
LEFT JOIN string_pool h ON i.host_id = h.id
LEFT JOIN string_pool u ON i.username_id = u.id
LEFT JOIN string_pool bu ON i.basic_username_id = bu.id;
