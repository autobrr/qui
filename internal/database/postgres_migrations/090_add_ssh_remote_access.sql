-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- SSH access for instances whose torrent data lives on another host.
-- ssh_key_encrypted holds the AES-GCM encrypted private key (AAD: instance id
-- + field). ssh_host_key_encrypted holds the pinned host key under the same
-- AEAD (AAD: instance id + field + host + port) so a plaintext edit that
-- redirects the instance fails as a decryption error rather than as a host-key
-- mismatch. The pinned value is the marshaled public key, which carries its own
-- algorithm name -- not a fingerprint: HostKeyAlgorithms pinning and the
-- mismatch flow both need the full key.
ALTER TABLE instances ADD COLUMN ssh_host               TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_port               INTEGER NOT NULL DEFAULT 22;
ALTER TABLE instances ADD COLUMN ssh_username           TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_key_encrypted      TEXT NOT NULL DEFAULT '';
ALTER TABLE instances ADD COLUMN ssh_host_key_encrypted TEXT NOT NULL DEFAULT '';

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
    i.ssh_key_encrypted,
    i.ssh_host_key_encrypted
FROM instances i
LEFT JOIN string_pool n ON i.name_id = n.id
LEFT JOIN string_pool h ON i.host_id = h.id
LEFT JOIN string_pool u ON i.username_id = u.id
LEFT JOIN string_pool bu ON i.basic_username_id = bu.id;
