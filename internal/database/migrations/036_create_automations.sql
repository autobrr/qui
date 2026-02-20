-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Consolidated migration: replaces tracker_rules with automations system
-- and adds local filesystem access tracking for hardlink detection.

-- Drop legacy tracker_rules objects from main branch
DROP TRIGGER IF EXISTS trg_tracker_rules_updated;
DROP INDEX IF EXISTS idx_tracker_rule_activity_instance_created;
DROP INDEX IF EXISTS idx_tracker_rules_instance;
DROP TABLE IF EXISTS tracker_rule_activity;
DROP TABLE IF EXISTS tracker_rules;

-- Create automations table (final schema)
CREATE TABLE IF NOT EXISTS automations (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id     INTEGER NOT NULL,
    name            TEXT NOT NULL,
    tracker_pattern TEXT NOT NULL,
    conditions      TEXT NOT NULL,
    enabled         INTEGER NOT NULL DEFAULT 1,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_automations_instance ON automations(instance_id, sort_order, id);

CREATE TRIGGER IF NOT EXISTS trg_automations_updated
AFTER UPDATE ON automations
BEGIN
    UPDATE automations
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;

-- Create automation_activity table for tracking rule executions
CREATE TABLE IF NOT EXISTS automation_activity (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id     INTEGER NOT NULL,
    hash            TEXT NOT NULL,
    torrent_name    TEXT,
    tracker_domain  TEXT,
    action          TEXT NOT NULL,
    rule_id         INTEGER,
    rule_name       TEXT,
    outcome         TEXT NOT NULL,
    reason          TEXT,
    details         TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_automation_activity_instance_created
    ON automation_activity(instance_id, created_at DESC);

-- Add local filesystem access flag for hardlink detection (opt-in, default false)
ALTER TABLE instances
ADD COLUMN has_local_filesystem_access BOOLEAN NOT NULL DEFAULT 0;

-- Recreate instances_view to expose the new column (based on migration 015)
DROP VIEW IF EXISTS instances_view;
CREATE VIEW instances_view AS
SELECT
    i.id,
    sp_name.value AS name,
    sp_host.value AS host,
    sp_username.value AS username,
    i.password_encrypted,
    sp_basic_username.value AS basic_username,
    i.basic_password_encrypted,
    i.tls_skip_verify,
    i.sort_order,
    i.is_active,
    i.has_local_filesystem_access
FROM instances i
INNER JOIN string_pool sp_name ON i.name_id = sp_name.id
INNER JOIN string_pool sp_host ON i.host_id = sp_host.id
INNER JOIN string_pool sp_username ON i.username_id = sp_username.id
LEFT JOIN string_pool sp_basic_username ON i.basic_username_id = sp_basic_username.id;
