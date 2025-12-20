-- +migrate Up
-- Drop legacy automation rules (those without expression-based conditions)
DELETE FROM automations WHERE conditions IS NULL OR conditions = '';

-- Create new table without legacy columns
CREATE TABLE automations_new (
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

-- Copy data from old table
INSERT INTO automations_new (id, instance_id, name, tracker_pattern, conditions, enabled, sort_order, created_at, updated_at)
SELECT id, instance_id, name, tracker_pattern, conditions, enabled, sort_order, created_at, updated_at
FROM automations;

-- Drop old table and rename new one
DROP TABLE automations;
ALTER TABLE automations_new RENAME TO automations;

-- Recreate index
CREATE INDEX idx_automations_instance_id ON automations(instance_id);

-- +migrate Down
-- Recreate old table with legacy columns
CREATE TABLE automations_old (
    id                          INTEGER PRIMARY KEY AUTOINCREMENT,
    instance_id                 INTEGER NOT NULL,
    name                        TEXT NOT NULL,
    tracker_pattern             TEXT NOT NULL,
    category                    TEXT,
    tag                         TEXT,
    tag_match_mode              TEXT DEFAULT 'any',
    upload_limit_kib            INTEGER,
    download_limit_kib          INTEGER,
    ratio_limit                 REAL,
    seeding_time_limit_minutes  INTEGER,
    delete_mode                 TEXT NOT NULL DEFAULT 'none',
    delete_unregistered         INTEGER NOT NULL DEFAULT 0,
    conditions                  TEXT,
    enabled                     INTEGER NOT NULL DEFAULT 1,
    sort_order                  INTEGER NOT NULL DEFAULT 0,
    created_at                  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at                  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
);

-- Copy data back (legacy columns will be NULL)
INSERT INTO automations_old (id, instance_id, name, tracker_pattern, conditions, enabled, sort_order, created_at, updated_at)
SELECT id, instance_id, name, tracker_pattern, conditions, enabled, sort_order, created_at, updated_at
FROM automations;

DROP TABLE automations;
ALTER TABLE automations_old RENAME TO automations;

CREATE INDEX idx_automations_instance_id ON automations(instance_id);
