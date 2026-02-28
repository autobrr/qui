-- Enforce log_exclusions singleton row.
-- Without this, concurrent initialization can create multiple rows and Get() can return arbitrary data.

DELETE FROM log_exclusions
WHERE id != (SELECT MIN(id) FROM log_exclusions);

UPDATE log_exclusions
SET id = 1;

-- Unique expression index on a constant enforces a maximum of one row.
CREATE UNIQUE INDEX IF NOT EXISTS idx_log_exclusions_singleton ON log_exclusions ((1));
