-- Copyright (c) 2026, s0up and the autobrr contributors.
-- SPDX-License-Identifier: GPL-2.0-or-later

CREATE TABLE IF NOT EXISTS media_id_cache (
    torrent_key TEXT NOT NULL,
    content_type TEXT NOT NULL CHECK(content_type IN ('movie', 'tv', 'anime')),
    extractor_version INTEGER NOT NULL,
    id_type TEXT,
    id_value TEXT,
    cached_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (torrent_key, content_type),
    CHECK ((id_type IS NULL) = (id_value IS NULL))
);
