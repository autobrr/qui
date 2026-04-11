CREATE TABLE IF NOT EXISTS cross_seed_indexer_categories (
    instance_id INTEGER NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
    indexer_id  INTEGER NOT NULL REFERENCES torznab_indexers(id) ON DELETE CASCADE,
    category    TEXT NOT NULL DEFAULT '',
    updated_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (instance_id, indexer_id)
);
