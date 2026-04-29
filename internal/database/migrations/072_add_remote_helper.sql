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
ALTER TABLE instances ADD COLUMN helper_deployed_at           DATETIME;
ALTER TABLE instances ADD COLUMN helper_last_activity_at      DATETIME;
