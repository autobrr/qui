-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Whether the server of a remote instance advertises hardlink@openssh.com,
-- as the last probe of the host found. NULL until a probe answers, and again
-- after a change that resets the tier. Drives a notice in the UI, never a gate:
-- the remote backend reads the live extension list when it links.
ALTER TABLE instances ADD COLUMN ssh_hardlink BOOLEAN;

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
    i.ssh_host_key_encrypted,
    i.ssh_tier,
    i.ssh_hardlink
FROM instances i
LEFT JOIN string_pool n ON i.name_id = n.id
LEFT JOIN string_pool h ON i.host_id = h.id
LEFT JOIN string_pool u ON i.username_id = u.id
LEFT JOIN string_pool bu ON i.basic_username_id = bu.id;
