-- Copyright (c) 2025, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Persist rate-limit cooldown windows for Torznab indexers so that restarts
-- retain the enforced waiting period and avoid repeated tracker bans.
CREATE TABLE IF NOT EXISTS torznab_indexer_cooldowns (
    indexer_id INTEGER PRIMARY KEY REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    resume_at TIMESTAMP NOT NULL,
    cooldown_seconds INTEGER NOT NULL,
    reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_torznab_cooldowns_resume
    ON torznab_indexer_cooldowns(resume_at);

CREATE TRIGGER IF NOT EXISTS trg_torznab_cooldowns_updated_at
AFTER UPDATE ON torznab_indexer_cooldowns
FOR EACH ROW
BEGIN
    UPDATE torznab_indexer_cooldowns
    SET updated_at = CURRENT_TIMESTAMP
    WHERE indexer_id = NEW.indexer_id;
END;
