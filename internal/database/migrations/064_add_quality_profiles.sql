-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add quality_profiles table for storing reusable quality ranking profiles.
-- Profiles are global (not per-instance) and referenced from automation conditions.

CREATE TABLE IF NOT EXISTS quality_profiles (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    group_fields TEXT NOT NULL DEFAULT '[]',   -- JSON array of rls field names used for grouping
    ranking_tiers TEXT NOT NULL DEFAULT '[]',  -- JSON array of RankingTier objects
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TRIGGER IF NOT EXISTS trg_quality_profiles_updated
AFTER UPDATE ON quality_profiles
BEGIN
    UPDATE quality_profiles
    SET updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.id;
END;
