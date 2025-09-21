# Database Migration Test Guide

The migration contract tests in `db_test.go` assert that applying every SQL file under `migrations/` produces a schema that matches the expectations captured in the test. When you add or modify migrations, update the test alongside the SQL changes so the schema contract stays accurate.

## Steps to update the migration tests

1. **Add your migration(s)** to `internal/database/migrations/NNN_description.sql` and ensure the numeric prefix is unique and sequential.
2. **Run `go test ./internal/database`** to execute the migration suite. The new migration will probably break one or more assertions—use the failure messages as a checklist.
3. **Update the expectation maps in `db_test.go`:**
   - `expectedSchema`: add or adjust column definitions for any tables you created or altered.
   - `expectedIndexes`: include new index names keyed by their table.
   - `expectedTriggers`: append trigger names you introduced.
4. **Add any new PRAGMA expectations** if the migration depends on additional SQLite settings.
5. **Re-run `go test ./internal/database`**. The suite should now pass and confirm that all migrations apply cleanly and that the database stays structurally sound.
