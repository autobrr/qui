-- Nothing reads last_run_id, and SQLite cannot drop a foreign key column, so rebuild the table without it.
CREATE TABLE cross_seed_feed_items_new (
    guid TEXT NOT NULL,
    indexer_id INTEGER NOT NULL,
    title TEXT,
    first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_status TEXT NOT NULL DEFAULT 'pending',
    info_hash TEXT,
    PRIMARY KEY (guid, indexer_id),
    FOREIGN KEY (indexer_id) REFERENCES torznab_indexers(id) ON DELETE CASCADE
);

INSERT INTO cross_seed_feed_items_new (
    guid, indexer_id, title, first_seen_at, last_seen_at, last_status, info_hash
)
SELECT guid, indexer_id, title, first_seen_at, last_seen_at, last_status, info_hash
FROM cross_seed_feed_items
-- Orphan rows would fail the foreign key check, and the indexer cascade would have removed them.
WHERE indexer_id IN (SELECT id FROM torznab_indexers);

DROP TABLE cross_seed_feed_items;

ALTER TABLE cross_seed_feed_items_new RENAME TO cross_seed_feed_items;

CREATE INDEX idx_cross_seed_feed_items_indexer ON cross_seed_feed_items(indexer_id);
CREATE INDEX idx_cross_seed_feed_items_last_seen ON cross_seed_feed_items(last_seen_at DESC);
