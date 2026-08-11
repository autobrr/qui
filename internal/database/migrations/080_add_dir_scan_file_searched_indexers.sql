-- Record which indexers a dir-scan search covered when it stamped no_match.
-- A NULL value means the row predates this column; such rows stay final.
ALTER TABLE dir_scan_files ADD COLUMN searched_indexer_ids TEXT;
