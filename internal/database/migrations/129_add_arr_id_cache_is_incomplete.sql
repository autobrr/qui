-- Copyright (c) 2025-2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

ALTER TABLE arr_id_cache
    ADD COLUMN is_incomplete INTEGER NOT NULL DEFAULT 0;

UPDATE arr_id_cache
SET is_incomplete = 1
WHERE cached_at IS NOT NULL
  AND datetime(expires_at) <= datetime(cached_at, '+11 minutes');
