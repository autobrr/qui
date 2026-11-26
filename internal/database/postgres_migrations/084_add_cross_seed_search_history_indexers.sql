-- Per-indexer cross-seed search history: one row per (instance, torrent, indexer)
-- records when that indexer last saw a search for that torrent, so cooldowns apply
-- per indexer and a newly added indexer backfills automatically (no rows = eligible).
CREATE TABLE cross_seed_search_history_indexers (
    instance_id INTEGER NOT NULL,
    torrent_hash TEXT NOT NULL,
    indexer_id INTEGER NOT NULL,
    last_searched_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, torrent_hash, indexer_id),
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
    FOREIGN KEY (indexer_id) REFERENCES torznab_indexers(id) ON DELETE CASCADE
);

CREATE INDEX idx_cross_seed_search_history_indexers_indexer
    ON cross_seed_search_history_indexers(indexer_id);

-- Seed from the existing per-torrent history, assuming past searches covered every
-- configured indexer (what the old code recorded), so upgrading does not make the
-- whole library instantly eligible for re-search. Pseudo-keys (season:, packfail:)
-- contain ':' and stay per-torrent in the old table.
INSERT INTO cross_seed_search_history_indexers (instance_id, torrent_hash, indexer_id, last_searched_at)
SELECT h.instance_id, h.torrent_hash, i.id, h.last_searched_at
FROM cross_seed_search_history h
CROSS JOIN torznab_indexers i
WHERE h.torrent_hash NOT LIKE '%:%';
