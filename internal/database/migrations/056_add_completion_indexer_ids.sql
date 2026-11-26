-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE instance_crossseed_completion_settings
    ADD COLUMN indexer_ids_json TEXT NOT NULL DEFAULT '[]';
