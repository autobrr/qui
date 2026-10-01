-- Nothing reads last_run_id, and its ON DELETE SET NULL rewrote feed rows on every run prune.

ALTER TABLE cross_seed_feed_items DROP COLUMN last_run_id;
