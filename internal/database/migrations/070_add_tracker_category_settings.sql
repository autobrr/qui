-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

-- Add per-instance "By Tracker" directory preset support.
-- When an instance's hardlink_dir_preset is "by-tracker", cross-seeds use the
-- category configured for each (instance, indexer) pair, falling back to the
-- global category mode (affix/indexer-name/custom/reuse).

-- Per-instance, per-indexer category mappings.
-- For a given qBittorrent instance + torznab indexer pair, stores which qBittorrent
-- category to assign (and whose save path determines the hardlink destination).
CREATE TABLE IF NOT EXISTS cross_seed_indexer_categories (
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    indexer_id  INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    category    TEXT NOT NULL DEFAULT '',
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, indexer_id)
);
