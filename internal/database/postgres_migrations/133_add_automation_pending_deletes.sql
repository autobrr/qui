-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS automation_pending_deletes (
    instance_id        INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    rule_id            INTEGER NOT NULL REFERENCES automations(id) ON DELETE CASCADE,
    torrent_hash       TEXT NOT NULL,
    unregistered_since TIMESTAMP NOT NULL,
    PRIMARY KEY (rule_id, torrent_hash)
);

CREATE INDEX IF NOT EXISTS idx_automation_pending_deletes_instance ON automation_pending_deletes(instance_id);
