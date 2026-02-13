-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add Gazelle (OPS/RED) settings for cross-seed.
-- These are used to power gzlx-style matching via Gazelle JSON APIs.

ALTER TABLE cross_seed_settings ADD COLUMN gazelle_enabled BOOLEAN NOT NULL DEFAULT 0;
ALTER TABLE cross_seed_settings ADD COLUMN redacted_api_key_encrypted TEXT NOT NULL DEFAULT '';
ALTER TABLE cross_seed_settings ADD COLUMN orpheus_api_key_encrypted TEXT NOT NULL DEFAULT '';
