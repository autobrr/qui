-- Nothing reads last_run_id, and its ON DELETE SET NULL made every run prune
-- rewrite each feed row that run wrote. Dropping the column drops its foreign key.

ALTER TABLE cross_seed_feed_items DROP COLUMN last_run_id;
