-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE arr_id_cache
    ADD COLUMN IF NOT EXISTS title_seasons_json TEXT;

UPDATE arr_id_cache
SET titles_json = NULL
WHERE content_type IN ('tv', 'anime');
