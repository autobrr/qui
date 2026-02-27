-- Enforce log_exclusions singleton row.
-- Without this, concurrent initialization can create multiple rows and Get() can return arbitrary data.

DELETE FROM log_exclusions
WHERE id <> (SELECT MIN(id) FROM log_exclusions);

UPDATE log_exclusions
SET id = 1;

DO $$
BEGIN
	ALTER TABLE log_exclusions ADD CONSTRAINT log_exclusions_singleton CHECK (id = 1);
EXCEPTION
	WHEN duplicate_object THEN NULL;
END $$;
